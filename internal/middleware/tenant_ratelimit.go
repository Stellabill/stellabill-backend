package middleware

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"stellarbill-backend/internal/timeutil"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

const (
	// Number of shards for the limiter map to reduce lock contention
	numShards = 32
	// TTL after which an idle limiter is evicted
	limiterTTL = 5 * time.Minute
)

// tenantLimiter holds a rate limiter and its last access time for a specific tenant
type tenantLimiter struct {
	limiter     *rate.Limiter
	lastAccess  time.Time
	mu          sync.Mutex
}

// shard holds a subset of tenant limiters
type shard struct {
	limiters map[string]*tenantLimiter
	mu      sync.RWMutex
}

// TenantRateLimiter manages per-tenant rate limiting with sharded storage and TTL eviction
type TenantRateLimiter struct {
	shards     []*shard
	rps        int
	burst      int
	evictionCh chan struct{}
}

// NewTenantRateLimiter creates a new per-tenant rate limiter
func NewTenantRateLimiter(rps, burst int) *TenantRateLimiter {
	trl := &TenantRateLimiter{
		shards:     make([]*shard, numShards),
		rps:        rps,
		burst:      burst,
		evictionCh: make(chan struct{}, 1),
	}

	// Initialize shards
	for i := 0; i < numShards; i++ {
		trl.shards[i] = &shard{
			limiters: make(map[string]*tenantLimiter),
		}
	}

	// Start background eviction goroutine
	go trl.evictionLoop()

	return trl
}

// getShard returns the shard for a given tenant ID
func (trl *TenantRateLimiter) getShard(tenantID string) *shard {
	// Simple hash-based sharding
	hash := 0
	for _, c := range tenantID {
		hash = hash*31 + int(c)
	}
	if hash < 0 {
		hash = -hash
	}
	return trl.shards[hash%numShards]
}

// getLimiter returns or creates a limiter for the given tenant ID
func (trl *TenantRateLimiter) getLimiter(tenantID string) *tenantLimiter {
	shard := trl.getShard(tenantID)
	
	shard.mu.RLock()
	limiter, exists := shard.limiters[tenantID]
	if exists {
		limiter.mu.Lock()
		limiter.lastAccess = timeutil.NowUTC()
		limiter.mu.Unlock()
		shard.mu.RUnlock()
		return limiter
	}
	shard.mu.RUnlock()

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists := shard.limiters[tenantID]; exists {
		limiter.mu.Lock()
		limiter.lastAccess = timeutil.NowUTC()
		limiter.mu.Unlock()
		return limiter
	}

	// Create new limiter
	limiter = &tenantLimiter{
		limiter:     rate.NewLimiter(rate.Limit(trl.rps), trl.burst),
		lastAccess:  timeutil.NowUTC(),
	}
	shard.limiters[tenantID] = limiter

	return limiter
}

// evictionLoop periodically evicts idle limiters
func (trl *TenantRateLimiter) evictionLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			trl.evictIdleLimiters()
		case <-trl.evictionCh:
			return
		}
	}
}

// evictIdleLimiters removes limiters that haven't been accessed within the TTL
func (trl *TenantRateLimiter) evictIdleLimiters() {
	now := timeutil.NowUTC()
	for _, shard := range trl.shards {
		shard.mu.Lock()
		for tenantID, limiter := range shard.limiters {
			limiter.mu.Lock()
			if now.Sub(limiter.lastAccess) > limiterTTL {
				delete(shard.limiters, tenantID)
			}
			limiter.mu.Unlock()
		}
		shard.mu.Unlock()
	}
}

// Stop stops the eviction goroutine
func (trl *TenantRateLimiter) Stop() {
	close(trl.evictionCh)
}

// Allow checks if a request from the given tenant is allowed
func (trl *TenantRateLimiter) Allow(tenantID string) bool {
	limiter := trl.getLimiter(tenantID)
	return limiter.limiter.Allow()
}

// TenantRateLimitConfig holds configuration for per-tenant rate limiting
type TenantRateLimitConfig struct {
	Enabled      bool
	RPS          int
	Burst        int
	LogRateLimitHits bool
}

// TenantRateLimitMiddleware creates a Gin middleware for per-tenant rate limiting
func TenantRateLimitMiddleware(config TenantRateLimitConfig) gin.HandlerFunc {
	if config.RPS <= 0 {
		config.RPS = 5 // Default: 5 requests per second per tenant
	}
	if config.Burst <= 0 {
		config.Burst = config.RPS * 2 // Default burst: 2x rate
	}

	limiter := NewTenantRateLimiter(config.RPS, config.Burst)

	return func(c *gin.Context) {
		// Skip rate limiting if disabled
		if !config.Enabled {
			c.Next()
			return
		}

		// Extract tenant ID from context (set by auth middleware)
		tenantID := "anonymous"
		if tid, exists := c.Get("tenantID"); exists {
			if tidStr, ok := tid.(string); ok {
				tenantID = tidStr
			}
		}

		// Check if request is allowed
		if !limiter.Allow(tenantID) {
			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", config.Burst))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", timeutil.FormatRFC3339UTC(timeutil.NowUTC().Add(time.Second)))
			c.Header("Retry-After", "1")

			// Log rate limit hit if enabled
			if config.LogRateLimitHits {
				log.Printf("[TENANT_RATE_LIMIT] tenant=%s path=%s", tenantID, c.Request.URL.Path)
			}

			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "tenant rate limit exceeded",
				"code":    "TENANT_RATE_LIMIT_EXCEEDED",
				"message": "Too many requests for this tenant. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
