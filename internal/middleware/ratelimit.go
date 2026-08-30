package middleware

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"stellarbill-backend/internal/timeutil"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimitSnapshot captures the current state of rate limiting for header emission
type RateLimitSnapshot struct {
	Limit     int64     // Maximum requests allowed in the current window
	Remaining int64     // Number of requests remaining in the current window
	Reset     time.Time // Time when the rate limit window resets
}

// emitRateLimitHeaders emits rate limit headers to the response
// Uses unix-seconds format for Reset and ensures no negative values
func emitRateLimitHeaders(c *gin.Context, snapshot RateLimitSnapshot) {
	// Ensure no negative values
	limit := snapshot.Limit
	if limit < 0 {
		limit = 0
	}

	remaining := snapshot.Remaining
	if remaining < 0 {
		remaining = 0
	}

	// Emit standard rate limit headers
	c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
	c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
	c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", snapshot.Reset.Unix()))

	// Calculate and emit Retry-After for rate-limited responses
	if remaining == 0 {
		retryAfter := int(math.Ceil(snapshot.Reset.Sub(timeutil.NowUTC()).Seconds()))
		if retryAfter < 1 {
			retryAfter = 1 // Minimum 1 second
		}
		c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
	}
}

// RateLimitMode defines the rate limiting strategy
type RateLimitMode string

const (
	ModeIP     RateLimitMode = "ip"     // Rate limit by client IP
	ModeUser   RateLimitMode = "user"   // Rate limit by authenticated user ID
	ModeHybrid RateLimitMode = "hybrid" // Rate limit by both IP and user (stricter)
)

// TokenBucket represents a token bucket for rate limiting
type TokenBucket struct {
	capacity      int64      // Maximum number of tokens
	tokens        int64      // Current number of tokens
	refillRate    int64      // Tokens added per second
	lastRefill    time.Time  // Last time tokens were refilled
	mutex         sync.Mutex // Thread-safe access
	burstCapacity int64      // Maximum burst capacity
}

// RouteSpecificConfig holds per-route rate limiting configuration
type RouteSpecificConfig struct {
	Path           string // Route path pattern
	RequestsPerSec int64  // Requests per second for this route
	BurstSize      int64  // Burst size for this route
}

// RateLimiterConfig holds configuration for rate limiting
type RateLimiterConfig struct {
	Mode             RateLimitMode                  // Rate limiting mode
	RequestsPerSec   int64                          // Base requests per second
	BurstSize        int64                          // Maximum burst size
	WhitelistPaths   []string                       // Paths to exclude from rate limiting
	Enabled          bool                           // Enable/disable rate limiting
	RouteConfigs     map[string]RouteSpecificConfig // Per-route overrides
	LogRateLimitHits bool                           // Log when rate limits are hit
}

// APIRateLimiter manages multiple token buckets for rate limiting
type APIRateLimiter struct {
	config  RateLimiterConfig
	buckets map[string]*TokenBucket
	mutex   sync.RWMutex
	stopCh  chan struct{}
}

// NewTokenBucket creates a new token bucket
func NewTokenBucket(capacity, refillRate, burstCapacity int64) *TokenBucket {
	// Start with burst capacity tokens, not just capacity
	return &TokenBucket{
		capacity:      capacity,
		tokens:        burstCapacity, // Start with burst capacity
		refillRate:    refillRate,
		burstCapacity: burstCapacity,
		lastRefill:    timeutil.NowUTC(),
	}
}

// refill adds tokens to the bucket based on elapsed time
func (tb *TokenBucket) refill() {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	now := timeutil.NowUTC()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tokensToAdd := int64(elapsed * float64(tb.refillRate))

	if tokensToAdd > 0 {
		tb.tokens += tokensToAdd
		if tb.tokens > tb.burstCapacity {
			tb.tokens = tb.burstCapacity
		}
		tb.lastRefill = now
	}
}

// allowRequest checks if a request is allowed based on available tokens
func (tb *TokenBucket) allowRequest() bool {
	tb.refill()

	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}

	return false
}

// NewAPIRateLimiter creates a new API rate limiter
func NewAPIRateLimiter(config RateLimiterConfig) *APIRateLimiter {
	rl := &APIRateLimiter{
		config:  config,
		buckets: make(map[string]*TokenBucket),
		stopCh:  make(chan struct{}),
	}

	if config.RouteConfigs == nil {
		rl.config.RouteConfigs = make(map[string]RouteSpecificConfig)
	}

	rl.startCleanupLoop()

	return rl
}

// Stop halts the background bucket cleanup loop.
func (rl *APIRateLimiter) Stop() {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()
	if rl.stopCh == nil {
		return
	}
	close(rl.stopCh)
	rl.stopCh = nil
}

// cleanupExpiredBuckets removes buckets that have been idle past the
// retention window to prevent unbounded memory growth.
func (rl *APIRateLimiter) cleanupExpiredBuckets() {
	retention := time.Minute
	now := timeutil.NowUTC()

	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	for k, bucket := range rl.buckets {
		bucket.mutex.Lock()
		idle := now.Sub(bucket.lastRefill)
		bucket.mutex.Unlock()

		if idle > retention {
			delete(rl.buckets, k)
		}
	}
}

// startCleanupLoop runs a periodic sweep goroutine until Stop is called.
func (rl *APIRateLimiter) startCleanupLoop() {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rl.cleanupExpiredBuckets()
			case <-rl.stopCh:
				return
			}
		}
	}()
}

// getBucket retrieves or creates a token bucket for the given key with route-specific config
func (rl *APIRateLimiter) getBucket(key string, path string) *TokenBucket {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	bucketKey := key + ":" + path
	if bucket, exists := rl.buckets[bucketKey]; exists {
		return bucket
	}

	// Check for route-specific config
	rps := rl.config.RequestsPerSec
	burst := rl.config.BurstSize

	if routeConfig, exists := rl.config.RouteConfigs[path]; exists {
		rps = routeConfig.RequestsPerSec
		burst = routeConfig.BurstSize
	}

	// Create new bucket with configured parameters
	bucket := NewTokenBucket(
		rps,
		rps,
		burst,
	)
	rl.buckets[bucketKey] = bucket
	return bucket
}

// getKey determines the rate limiting key based on mode and request
func (rl *APIRateLimiter) getKey(c *gin.Context) string {
	switch rl.config.Mode {
	case ModeIP:
		return getClientIP(c)
	case ModeUser:
		if userID, exists := c.Get("callerID"); exists {
			return userID.(string)
		}
		// Fallback to IP if user not authenticated
		return getClientIP(c)
	case ModeHybrid:
		userID := "anonymous"
		if uid, exists := c.Get("callerID"); exists {
			userID = uid.(string)
		}
		return userID + ":" + getClientIP(c)
	default:
		return getClientIP(c)
	}
}

// getClientIP extracts the real client IP, considering proxies
func getClientIP(c *gin.Context) string {
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		for i, ch := range xff {
			if ch == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	if xri := c.GetHeader("X-Real-IP"); xri != "" {
		return xri
	}
	return c.ClientIP()
}

// isWhitelisted checks if a path should be excluded from rate limiting
func (rl *APIRateLimiter) isWhitelisted(path string) bool {
	for _, whitelistPath := range rl.config.WhitelistPaths {
		if path == whitelistPath {
			return true
		}
	}
	return false
}

// RateLimitMiddleware creates a Gin middleware for rate limiting
func RateLimitMiddleware(config RateLimiterConfig) gin.HandlerFunc {
	// Set default values if not provided
	if config.RequestsPerSec <= 0 {
		config.RequestsPerSec = 10 // Default: 10 requests per second
	}
	if config.BurstSize <= 0 {
		config.BurstSize = config.RequestsPerSec * 2 // Default burst: 2x rate
	}
	if config.Mode == "" {
		config.Mode = ModeIP // Default mode
	}

	limiter := NewAPIRateLimiter(config)

	return func(c *gin.Context) {
		// Skip rate limiting if disabled or path is whitelisted
		if !config.Enabled || limiter.isWhitelisted(c.Request.URL.Path) {
			c.Next()
			return
		}

		key := limiter.getKey(c)
		path := c.Request.URL.Path
		bucket := limiter.getBucket(key, path)

		if !bucket.allowRequest() {
			// Rate limit exceeded - calculate reset time based on refill rate
			bucket.mutex.Lock()
			resetTime := bucket.lastRefill.Add(time.Duration(float64(bucket.capacity-bucket.tokens)/float64(bucket.refillRate)) * time.Second)
			if resetTime.Before(timeutil.NowUTC()) {
				resetTime = timeutil.NowUTC().Add(time.Second)
			}
			bucket.mutex.Unlock()

			emitRateLimitHeaders(c, RateLimitSnapshot{
				Limit:     bucket.burstCapacity,
				Remaining: 0,
				Reset:     resetTime,
			})

			// Log rate limit hit if enabled
			if config.LogRateLimitHits {
				log.Printf("[RATE_LIMIT] path=%s key=%s mode=%s", path, key, config.Mode)
			}

			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate limit exceeded",
				"code":    "RATE_LIMIT_EXCEEDED",
				"message": "Too many requests. Please try again later.",
			})
			c.Abort()
			return
		}

		// Add rate limit headers for successful requests
		bucket.mutex.Lock()
		remaining := bucket.tokens
		limit := bucket.burstCapacity
		// Calculate reset time based on when tokens will fully refill
		resetTime := bucket.lastRefill.Add(time.Duration(float64(limit-remaining)/float64(bucket.refillRate)) * time.Second)
		if resetTime.Before(timeutil.NowUTC()) {
			resetTime = timeutil.NowUTC().Add(time.Second)
		}
		bucket.mutex.Unlock()

		emitRateLimitHeaders(c, RateLimitSnapshot{
			Limit:     limit,
			Remaining: remaining,
			Reset:     resetTime,
		})

		c.Next()
	}
}
