package middleware

import (
	"log"
	"net/http"
	"stellarbill-backend/internal/timeutil"
	"sync"
	"time"

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
	limiter    *rate.Limiter
	lastAccess time.Time
	mu         sync.Mutex
}

// shard holds a subset of tenant limiters
type shard struct {
	limiters map[string]*tenantLimiter
	mu       sync.RWMutex
}

// TenantRateLimiter manages per-tenant rate limiting with sharded storage and TTL eviction
type TenantRateLimiter struct {
	shards     []*shard
	rps        int
	burst      int
	idleTTL    time.Duration
	evictionCh chan struct{}
	stopOnce   sync.Once
}

// NewTenantRateLimiter creates a new per-tenant rate limiter with the
// default idle TTL.
func NewTenantRateLimiter(rps, burst int) *TenantRateLimiter {
	return NewTenantRateLimiterWithIdleTTL(rps, burst, limiterTTL)
}

// NewTenantRateLimiterWithIdleTTL creates a new per-tenant rate limiter with a
// custom idle TTL for eviction. A non-positive TTL falls back to the default.
func NewTenantRateLimiterWithIdleTTL(rps, burst int, idleTTL time.Duration) *TenantRateLimiter {
	if idleTTL <= 0 {
		idleTTL = limiterTTL
	}

	trl := &TenantRateLimiter{
		shards:     make([]*shard, numShards),
		rps:        rps,
		burst:      burst,
		idleTTL:    idleTTL,
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
		limiter:    rate.NewLimiter(rate.Limit(trl.rps), trl.burst),
		lastAccess: timeutil.NowUTC(),
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
			if now.Sub(limiter.lastAccess) > trl.idleTTL {
				delete(shard.limiters, tenantID)
			}
			limiter.mu.Unlock()
		}
		shard.mu.Unlock()
	}
}

// Len reports the number of tenants currently holding an active limiter. It
// is meant for diagnostics and tests (e.g. asserting idle eviction frees
// memory).
func (trl *TenantRateLimiter) Len() int {
	total := 0
	for _, shard := range trl.shards {
		shard.mu.RLock()
		total += len(shard.limiters)
		shard.mu.RUnlock()
	}
	return total
}

// Stop stops the eviction goroutine
func (trl *TenantRateLimiter) Stop() {
	trl.stopOnce.Do(func() {
		close(trl.evictionCh)
	})
}

// Allow checks if a request from the given tenant is allowed
func (trl *TenantRateLimiter) Allow(tenantID string) bool {
	limiter := trl.getLimiter(tenantID)
	return limiter.limiter.Allow()
}

// getRateLimitSnapshot returns a snapshot of the current rate limit state
func (trl *TenantRateLimiter) getRateLimitSnapshot(tenantID string) RateLimitSnapshot {
	limiter := trl.getLimiter(tenantID)
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	// The golang.org/x/time/rate.Limiter doesn't directly expose remaining tokens
	// We estimate based on the burst size and the limiter's state
	burst := int64(limiter.limiter.Burst())
	limit := int64(limiter.limiter.Limit())

	// Estimate remaining tokens based on time since last access
	// This is an approximation since the limiter doesn't expose exact token count
	elapsed := timeutil.NowUTC().Sub(limiter.lastAccess).Seconds()
	tokensToAdd := int64(elapsed * float64(limit))
	remaining := burst - tokensToAdd
	if remaining < 0 {
		remaining = 0
	}
	if remaining > burst {
		remaining = burst
	}

	// Calculate reset time - when tokens will be fully replenished
	tokensNeeded := burst - remaining
	resetTime := timeutil.NowUTC()
	if tokensNeeded > 0 && limit > 0 {
		secondsToRefill := float64(tokensNeeded) / float64(limit)
		resetTime = resetTime.Add(time.Duration(secondsToRefill) * time.Second)
	} else {
		resetTime = resetTime.Add(time.Second)
	}

	return RateLimitSnapshot{
		Limit:     burst,
		Remaining: remaining,
		Reset:     resetTime,
	}
}

// TenantRateLimitConfig holds configuration for per-tenant rate limiting
type TenantRateLimitConfig struct {
	Enabled          bool
	RPS              int
	Burst            int
	LogRateLimitHits bool
	// IdleTTL controls how long an unused tenant limiter is kept before it is
	// evicted. Zero means the default (5 minutes).
	IdleTTL time.Duration
}

// TenantRateLimitMiddleware creates a Gin middleware for per-tenant rate limiting
func TenantRateLimitMiddleware(config TenantRateLimitConfig) gin.HandlerFunc {
	// Disabled middleware is a no-op and does not spawn an eviction goroutine.
	if !config.Enabled {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	if config.RPS <= 0 {
		config.RPS = 5 // Default: 5 requests per second per tenant
	}
	if config.Burst <= 0 {
		config.Burst = config.RPS * 2 // Default burst: 2x rate
	}

	limiter := NewTenantRateLimiterWithIdleTTL(config.RPS, config.Burst, config.IdleTTL)

	return func(c *gin.Context) {
		// Extract tenant ID from context (set by auth middleware)
		tenantID := "anonymous"
		if tid, exists := c.Get("tenantID"); exists {
			if tidStr, ok := tid.(string); ok {
				tenantID = tidStr
			}
		}

		// Check if request is allowed
		if !limiter.Allow(tenantID) {
			// Get rate limit snapshot for headers
			snapshot := limiter.getRateLimitSnapshot(tenantID)
			snapshot.Remaining = 0 // Force to 0 for rate-limited response

			emitRateLimitHeaders(c, snapshot)

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

		// Emit rate limit headers for successful requests
		snapshot := limiter.getRateLimitSnapshot(tenantID)
		emitRateLimitHeaders(c, snapshot)

		c.Next()
	}
}
