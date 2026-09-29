package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestTenantRateLimiter_Allow(t *testing.T) {
	limiter := NewTenantRateLimiter(5, 10)
	defer limiter.Stop()

	tests := []struct {
		name     string
		tenantID string
		requests int
		expected int // allowed requests
	}{
		{
			name:     "allow requests within limit",
			tenantID: "tenant-1",
			requests: 5,
			expected: 5,
		},
		{
			name:     "deny requests exceeding burst",
			tenantID: "tenant-2",
			requests: 15,
			expected: 10,
		},
		{
			name:     "different tenants have separate limits",
			tenantID: "tenant-3",
			requests: 5,
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed := 0
			for i := 0; i < tt.requests; i++ {
				if limiter.Allow(tt.tenantID) {
					allowed++
				}
			}
			if allowed != tt.expected {
				t.Errorf("expected %d allowed requests, got %d", tt.expected, allowed)
			}
		})
	}
}

func TestTenantRateLimiter_Sharding(t *testing.T) {
	limiter := NewTenantRateLimiter(5, 10)
	defer limiter.Stop()

	// Test that different tenant IDs hash to different shards
	tenants := []string{"tenant-1", "tenant-2", "tenant-3", "tenant-4", "tenant-5"}

	for _, tenant := range tenants {
		limiter.Allow(tenant)
	}

	// Verify that each tenant has its own limiter
	for _, tenant := range tenants {
		shard := limiter.getShard(tenant)
		shard.mu.RLock()
		_, exists := shard.limiters[tenant]
		shard.mu.RUnlock()
		if !exists {
			t.Errorf("tenant %s should have a limiter", tenant)
		}
	}
}

func TestTenantRateLimiter_Eviction(t *testing.T) {
	// Create limiter with short TTL for testing
	limiter := NewTenantRateLimiter(5, 10)
	defer limiter.Stop()

	// Add a limiter for a tenant
	limiter.Allow("test-tenant")

	// Verify it exists
	shard := limiter.getShard("test-tenant")
	shard.mu.RLock()
	_, exists := shard.limiters["test-tenant"]
	shard.mu.RUnlock()
	if !exists {
		t.Fatal("limiter should exist after creation")
	}

	// Manually set last access to the past
	shard.mu.Lock()
	if limiter, exists := shard.limiters["test-tenant"]; exists {
		limiter.mu.Lock()
		limiter.lastAccess = time.Now().Add(-limiterTTL - 1*time.Minute)
		limiter.mu.Unlock()
	}
	shard.mu.Unlock()

	// Trigger eviction
	limiter.evictIdleLimiters()

	// Verify it was evicted
	shard.mu.RLock()
	_, exists = shard.limiters["test-tenant"]
	shard.mu.RUnlock()
	if exists {
		t.Error("limiter should be evicted after TTL")
	}
}

func TestTenantRateLimiter_ConcurrentAccess(t *testing.T) {
	limiter := NewTenantRateLimiter(10, 200)
	defer limiter.Stop()

	var wg sync.WaitGroup
	tenantID := "concurrent-tenant"
	requestsPerGoroutine := 10
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				limiter.Allow(tenantID)
			}
		}()
	}

	wg.Wait()

	time.Sleep(100 * time.Millisecond)

	// Verify the limiter still works after concurrent access
	allowed := limiter.Allow(tenantID)
	if !allowed {
		t.Error("limiter should allow request after concurrent access")
	}
}

func TestTenantRateLimitMiddleware_Anonymous(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := TenantRateLimitConfig{
		Enabled: true,
		RPS:     1,
		Burst:   2,
	}
	middleware := TenantRateLimitMiddleware(config)

	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Anonymous requests (no tenantID in context) share a single bucket:
	// burst (2) pass, then every subsequent anonymous request is rejected.
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)

		if i < 2 {
			if w.Code != http.StatusOK {
				t.Fatalf("request %d: expected OK (anonymous burst), got %d", i, w.Code)
			}
		} else {
			if w.Code != http.StatusTooManyRequests {
				t.Fatalf("request %d: expected 429 (shared anonymous bucket), got %d", i, w.Code)
			}
		}
	}
}

func TestTenantRateLimitMiddleware_WithTenantID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := TenantRateLimitConfig{
		Enabled: true,
		RPS:     5,
		Burst:   10,
	}
	middleware := TenantRateLimitMiddleware(config)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenantID", "test-tenant")
		c.Next()
	})
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Test requests with tenantID
	allowed := 0
	for i := 0; i < 15; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			allowed++
		}
	}

	if allowed != 10 {
		t.Errorf("expected 10 allowed requests, got %d", allowed)
	}
}

func TestTenantRateLimitMiddleware_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := TenantRateLimitConfig{
		Enabled: false,
		RPS:     5,
		Burst:   10,
	}
	middleware := TenantRateLimitMiddleware(config)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenantID", "test-tenant")
		c.Next()
	})
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Test that all requests are allowed when disabled
	for i := 0; i < 20; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected status OK when disabled, got %d", i, w.Code)
		}
	}
}

func TestTenantRateLimitMiddleware_TwoTenants(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := TenantRateLimitConfig{
		Enabled: true,
		RPS:     5,
		Burst:   10,
	}
	middleware := TenantRateLimitMiddleware(config)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		c.Set("tenantID", c.Query("tenant"))
		c.Next()
	}, middleware, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Test that two tenants don't contend
	tenant1Requests := 0
	tenant2Requests := 0

	for i := 0; i < 20; i++ {
		// Tenant 1 requests
		w1 := httptest.NewRecorder()
		req1, _ := http.NewRequest("GET", "/test?tenant=tenant-1", nil)
		router.ServeHTTP(w1, req1)
		if w1.Code == http.StatusOK {
			tenant1Requests++
		}

		// Tenant 2 requests
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest("GET", "/test?tenant=tenant-2", nil)
		router.ServeHTTP(w2, req2)
		if w2.Code == http.StatusOK {
			tenant2Requests++
		}
	}

	// Each tenant should get their full burst allowance
	if tenant1Requests != 10 {
		t.Errorf("tenant-1 expected 10 allowed requests, got %d", tenant1Requests)
	}
	if tenant2Requests != 10 {
		t.Errorf("tenant-2 expected 10 allowed requests, got %d", tenant2Requests)
	}
}

func TestTenantRateLimiter_Stop(t *testing.T) {
	limiter := NewTenantRateLimiter(5, 10)

	// Should not panic
	limiter.Stop()

	// Calling stop again should not panic
	limiter.Stop()
}

func TestTenantRateLimiter_IdleEvictionFreesMemory(t *testing.T) {
	limiter := NewTenantRateLimiter(5, 10)
	defer limiter.Stop()

	if got := limiter.Len(); got != 0 {
		t.Fatalf("expected no active tenants on a fresh limiter, got %d", got)
	}

	for _, tenant := range []string{"t-1", "t-2", "t-3"} {
		limiter.Allow(tenant)
	}
	if got := limiter.Len(); got != 3 {
		t.Fatalf("expected 3 active tenants after traffic, got %d", got)
	}

	// Age every limiter past the idle TTL, then run eviction.
	for i := 0; i < numShards; i++ {
		shard := limiter.shards[i]
		shard.mu.Lock()
		for _, tl := range shard.limiters {
			tl.mu.Lock()
			tl.lastAccess = time.Now().Add(-limiter.idleTTL - time.Minute)
			tl.mu.Unlock()
		}
		shard.mu.Unlock()
	}
	limiter.evictIdleLimiters()

	if got := limiter.Len(); got != 0 {
		t.Fatalf("expected idle eviction to release all limiters, got %d active", got)
	}
}

func TestTenantRateLimiter_ZeroIdleTTLFallsBackToDefault(t *testing.T) {
	limiter := NewTenantRateLimiterWithIdleTTL(5, 10, 0)
	defer limiter.Stop()

	if limiter.idleTTL != limiterTTL {
		t.Fatalf("expected default idle TTL %s, got %s", limiterTTL, limiter.idleTTL)
	}
}

func TestTenantRateLimitMiddleware_DefaultValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := TenantRateLimitConfig{
		Enabled: true,
		// RPS and Burst not set, should use defaults
	}
	middleware := TenantRateLimitMiddleware(config)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenantID", "test-tenant")
		c.Next()
	})
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Should use default RPS=5, Burst=10
	allowed := 0
	for i := 0; i < 15; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			allowed++
		}
	}

	if allowed != 10 {
		t.Errorf("expected 10 allowed requests with defaults, got %d", allowed)
	}
}

func TestTenantRateLimiter_Refill(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := NewTenantRateLimiter(5, 5)
	defer limiter.Stop()

	tenantID := "refill-tenant"

	// Consume all burst tokens
	for i := 0; i < 5; i++ {
		if !limiter.Allow(tenantID) {
			t.Fatal("should allow request within burst")
		}
	}

	// Next request should be denied
	if limiter.Allow(tenantID) {
		t.Error("should deny request after burst is consumed")
	}

	// Wait for refill
	time.Sleep(250 * time.Millisecond) // Wait for 0.25 seconds at 5 RPS = ~1 token

	// Should allow request after refill
	if !limiter.Allow(tenantID) {
		t.Error("should allow request after refill")
	}
}

func TestTenantRateLimitHeaders_UnixSecondsFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := TenantRateLimitConfig{
		Enabled: true,
		RPS:     5,
		Burst:   10,
	}
	middleware := TenantRateLimitMiddleware(config)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenantID", "test-tenant")
		c.Next()
	})
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Verify Reset header is in unix-seconds format
	resetStr := w.Header().Get("X-RateLimit-Reset")
	assert.NotEmpty(t, resetStr)

	resetUnix, err := strconv.ParseInt(resetStr, 10, 64)
	assert.NoError(t, err, "Reset should be unix-seconds (integer)")

	// Verify it's a reasonable timestamp (current time + future)
	now := time.Now().Unix()
	assert.Greater(t, resetUnix, now, "Reset should be in the future")
	assert.Less(t, resetUnix, now+3600, "Reset should be within a reasonable timeframe")
}

func TestTenantRateLimitHeaders_RetryAfterCalculation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := TenantRateLimitConfig{
		Enabled: true,
		RPS:     2,
		Burst:   2,
	}
	middleware := TenantRateLimitMiddleware(config)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenantID", "test-tenant")
		c.Next()
	})
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Exhaust the rate limit
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Next request should be rate limited with Retry-After
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	retryAfterStr := w.Header().Get("Retry-After")
	assert.NotEmpty(t, retryAfterStr, "Retry-After should be present on 429")

	retryAfter, err := strconv.Atoi(retryAfterStr)
	assert.NoError(t, err, "Retry-After should be an integer")
	assert.GreaterOrEqual(t, retryAfter, 1, "Retry-After should be at least 1 second")
	assert.LessOrEqual(t, retryAfter, 60, "Retry-After should be reasonable")
}

func TestTenantRateLimitHeaders_NoNegativeValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := TenantRateLimitConfig{
		Enabled: true,
		RPS:     1,
		Burst:   1,
	}
	middleware := TenantRateLimitMiddleware(config)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenantID", "test-tenant")
		c.Next()
	})
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Test successful response headers
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	limitStr := w.Header().Get("X-RateLimit-Limit")
	remainingStr := w.Header().Get("X-RateLimit-Remaining")

	limit, _ := strconv.ParseInt(limitStr, 10, 64)
	remaining, _ := strconv.ParseInt(remainingStr, 10, 64)

	assert.GreaterOrEqual(t, limit, int64(0), "Limit should never be negative")
	assert.GreaterOrEqual(t, remaining, int64(0), "Remaining should never be negative")

	// Test rate-limited response headers
	req2 := httptest.NewRequest("GET", "/test", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	limitStr2 := w2.Header().Get("X-RateLimit-Limit")
	remainingStr2 := w2.Header().Get("X-RateLimit-Remaining")

	limit2, _ := strconv.ParseInt(limitStr2, 10, 64)
	remaining2, _ := strconv.ParseInt(remainingStr2, 10, 64)

	assert.GreaterOrEqual(t, limit2, int64(0), "Limit should never be negative on 429")
	assert.GreaterOrEqual(t, remaining2, int64(0), "Remaining should never be negative on 429")
}

func TestTenantRateLimitHeaders_SuccessfulResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := TenantRateLimitConfig{
		Enabled: true,
		RPS:     5,
		Burst:   10,
	}
	middleware := TenantRateLimitMiddleware(config)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenantID", "test-tenant")
		c.Next()
	})
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Verify all headers are present
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Limit"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"))
	assert.Empty(t, w.Header().Get("Retry-After"), "Retry-After should not be present on successful requests")

	// Verify values are reasonable
	limit, _ := strconv.ParseInt(w.Header().Get("X-RateLimit-Limit"), 10, 64)
	remaining, _ := strconv.ParseInt(w.Header().Get("X-RateLimit-Remaining"), 10, 64)

	assert.Equal(t, int64(10), limit, "Limit should match burst size")
	assert.GreaterOrEqual(t, remaining, int64(0), "Remaining should be non-negative")
	assert.LessOrEqual(t, remaining, limit, "Remaining should not exceed limit")
}

func TestTenantRateLimiter_GetRateLimitSnapshot(t *testing.T) {
	limiter := NewTenantRateLimiter(5, 10)
	defer limiter.Stop()

	snapshot := limiter.getRateLimitSnapshot("test-tenant")

	assert.Greater(t, snapshot.Limit, int64(0), "Limit should be positive")
	assert.GreaterOrEqual(t, snapshot.Remaining, int64(0), "Remaining should be non-negative")
	assert.LessOrEqual(t, snapshot.Remaining, snapshot.Limit, "Remaining should not exceed limit")
	assert.True(t, snapshot.Reset.After(time.Now()), "Reset should be in the future")
}
