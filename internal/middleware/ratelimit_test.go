package middleware

import (
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestTokenBucket_AllowRequest(t *testing.T) {
	tb := NewTokenBucket(5, 10, 10) // 5 capacity, 10 refill rate, 10 burst

	// Should allow 10 initial requests (burst capacity)
	for i := 0; i < 10; i++ {
		assert.True(t, tb.allowRequest(), "Request %d should be allowed", i+1)
	}

	// 11th request should be denied
	assert.False(t, tb.allowRequest(), "11th request should be denied")

	// Wait for refill and test again
	time.Sleep(200 * time.Millisecond) // Allow some tokens to refill
	assert.True(t, tb.allowRequest(), "Request after refill should be allowed")
}

func TestTokenBucket_Refill(t *testing.T) {
	tb := NewTokenBucket(10, 5, 10) // 10 capacity, 5 refill rate per second

	// Use all tokens
	for i := 0; i < 10; i++ {
		tb.allowRequest()
	}

	// Should be empty now
	assert.False(t, tb.allowRequest(), "Should be empty after using all tokens")

	// Wait for refill
	time.Sleep(300 * time.Millisecond) // Should refill ~1.5 tokens

	// Should allow at least one request
	assert.True(t, tb.allowRequest(), "Should allow request after refill")
}

func TestTokenBucket_BurstCapacity(t *testing.T) {
	tb := NewTokenBucket(5, 1, 10) // 5 capacity, 1 refill rate, 10 burst

	// Initially should have 10 tokens (burst capacity)
	for i := 0; i < 10; i++ {
		assert.True(t, tb.allowRequest(), "Initial request %d should be allowed", i+1)
	}

	// 11th should be denied
	assert.False(t, tb.allowRequest(), "11th request should be denied")

	// Wait for refill and test again
	time.Sleep(2 * time.Second) // Should refill ~2 tokens
	assert.True(t, tb.allowRequest(), "Request after refill should be allowed")
}

func TestRateLimiter_GetKey_IPMode(t *testing.T) {
	config := RateLimiterConfig{
		Mode: ModeIP,
	}
	rl := NewAPIRateLimiter(config)

	// Test with different client IPs
	testCases := []struct {
		name        string
		headers     map[string]string
		remoteAddr  string
		expectedKey string
	}{
		{
			name:        "Direct IP",
			remoteAddr:  "192.168.1.100:12345",
			expectedKey: "192.168.1.100",
		},
		{
			name: "X-Forwarded-For",
			headers: map[string]string{
				"X-Forwarded-For": "10.0.0.1, 192.168.1.1",
			},
			remoteAddr:  "192.168.1.100:12345",
			expectedKey: "10.0.0.1",
		},
		{
			name: "X-Real-IP",
			headers: map[string]string{
				"X-Real-IP": "172.16.0.1",
			},
			remoteAddr:  "192.168.1.100:12345",
			expectedKey: "172.16.0.1",
		},
		{
			name: "X-Forwarded-For priority over X-Real-IP",
			headers: map[string]string{
				"X-Forwarded-For": "10.0.0.2",
				"X-Real-IP":       "172.16.0.2",
			},
			remoteAddr:  "192.168.1.100:12345",
			expectedKey: "10.0.0.2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/", nil)
			c.Request.RemoteAddr = tc.remoteAddr

			for key, value := range tc.headers {
				c.Request.Header.Set(key, value)
			}

			key := rl.getKey(c)
			assert.Equal(t, tc.expectedKey, key)
		})
	}
}

func TestRateLimiter_GetKey_UserMode(t *testing.T) {
	config := RateLimiterConfig{
		Mode: ModeUser,
	}
	rl := NewAPIRateLimiter(config)

	// Test with authenticated user
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.RemoteAddr = "192.168.1.100:12345"
	c.Set("callerID", "user123")

	key := rl.getKey(c)
	assert.Equal(t, "user123", key)

	// Test with anonymous user (should fallback to IP)
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest("GET", "/", nil)
	c2.Request.RemoteAddr = "192.168.1.200:12345"

	key2 := rl.getKey(c2)
	assert.Equal(t, "192.168.1.200", key2)
}

func TestRateLimiter_GetKey_HybridMode(t *testing.T) {
	config := RateLimiterConfig{
		Mode: ModeHybrid,
	}
	rl := NewAPIRateLimiter(config)

	// Test with authenticated user
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.RemoteAddr = "192.168.1.100:12345"
	c.Set("callerID", "user123")

	key := rl.getKey(c)
	assert.Equal(t, "user123:192.168.1.100", key)

	// Test with anonymous user
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest("GET", "/", nil)
	c2.Request.RemoteAddr = "192.168.1.200:12345"

	key2 := rl.getKey(c2)
	assert.Equal(t, "anonymous:192.168.1.200", key2)
}

func TestRateLimiter_Whitelist(t *testing.T) {
	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeIP,
		RequestsPerSec: 1,
		BurstSize:      1,
		WhitelistPaths: []string{"/api/health", "/api/status"},
	}
	rl := NewAPIRateLimiter(config)

	// Test whitelisted paths
	testCases := []struct {
		path       string
		shouldPass bool
	}{
		{"/api/health", true},         // Whitelisted, should pass
		{"/api/status", true},         // Whitelisted, should pass
		{"/api/subscriptions", false}, // Not whitelisted, should be rate limited
		{"/api/plans", false},         // Not whitelisted, should be rate limited
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			assert.Equal(t, tc.shouldPass, rl.isWhitelisted(tc.path),
				"Path %s whitelist check failed", tc.path)
		})
	}
}

func TestRateLimitMiddleware_BasicFunctionality(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeIP,
		RequestsPerSec: 2,
		BurstSize:      2,
		WhitelistPaths: []string{},
	}

	middleware := RateLimitMiddleware(config)
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	// Should allow first 2 requests
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code, "Request %d should succeed", i+1)
	}

	// Third request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 429, w.Code, "Third request should be rate limited")

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "rate limit exceeded", response["error"])
	assert.Equal(t, "RATE_LIMIT_EXCEEDED", response["code"])
}

func TestRateLimitMiddleware_DifferentIPs(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeIP,
		RequestsPerSec: 1,
		BurstSize:      1,
		WhitelistPaths: []string{},
	}

	middleware := RateLimitMiddleware(config)
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	// First IP should be rate limited after first request
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.100:12345"
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)

	req1Again := httptest.NewRequest("GET", "/test", nil)
	req1Again.RemoteAddr = "192.168.1.100:12345"
	w1Again := httptest.NewRecorder()
	router.ServeHTTP(w1Again, req1Again)
	assert.Equal(t, 429, w1Again.Code)

	// Different IP should still work
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.200:12345"
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)
}

func TestRateLimitMiddleware_WhitelistedPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeIP,
		RequestsPerSec: 1,
		BurstSize:      1,
		WhitelistPaths: []string{"/api/health"},
	}

	middleware := RateLimitMiddleware(config)
	router := gin.New()
	router.Use(middleware)
	router.GET("/api/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})
	router.GET("/api/subscriptions", func(c *gin.Context) {
		c.JSON(200, gin.H{"data": "subscriptions"})
	})

	// Whitelisted path should not be rate limited
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/api/health", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code, "Whitelisted path request %d should succeed", i+1)
	}

	// Non-whitelisted path should be rate limited
	req := httptest.NewRequest("GET", "/api/subscriptions", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	reqAgain := httptest.NewRequest("GET", "/api/subscriptions", nil)
	reqAgain.RemoteAddr = "192.168.1.100:12345"
	wAgain := httptest.NewRecorder()
	router.ServeHTTP(wAgain, reqAgain)
	assert.Equal(t, 429, wAgain.Code)
}

func TestRateLimitMiddleware_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        false,
		Mode:           ModeIP,
		RequestsPerSec: 1,
		BurstSize:      1,
		WhitelistPaths: []string{},
	}

	middleware := RateLimitMiddleware(config)
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	// Should allow unlimited requests when disabled
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code, "Request %d should succeed when rate limiting is disabled", i+1)
	}
}

func TestRateLimitMiddleware_ConcurrentAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeIP,
		RequestsPerSec: 10,
		BurstSize:      10,
		WhitelistPaths: []string{},
	}

	middleware := RateLimitMiddleware(config)
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	var wg sync.WaitGroup
	successCount := 0
	failCount := 0
	mu := sync.Mutex{}

	// Launch 20 concurrent requests
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "192.168.1.100:12345"
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			mu.Lock()
			if w.Code == 200 {
				successCount++
			} else if w.Code == 429 {
				failCount++
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Should have some successes and some failures
	assert.Greater(t, successCount, 0, "Should have some successful requests")
	assert.Greater(t, failCount, 0, "Should have some rate-limited requests")
	assert.Equal(t, 20, successCount+failCount, "Total requests should match")
}

func TestRateLimitMiddleware_RateLimitHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeIP,
		RequestsPerSec: 5,
		BurstSize:      5,
		WhitelistPaths: []string{},
	}

	middleware := RateLimitMiddleware(config)
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Check rate limit headers
	assert.Equal(t, "5", w.Header().Get("X-RateLimit-Limit"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"))

	// Test rate limited response headers
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.100:12345"
	w2 := httptest.NewRecorder()

	// Exhaust the bucket
	for i := 0; i < 5; i++ {
		router.ServeHTTP(w2, req2)
	}

	// Next request should be rate limited with proper headers
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "192.168.1.100:12345"
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)

	assert.Equal(t, 429, w3.Code)
	assert.Equal(t, "0", w3.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "0", w3.Header().Get("X-RateLimit-Remaining"))
	assert.Equal(t, "1", w3.Header().Get("Retry-After"))
}

func TestRateLimiter_CleanupExpiredBuckets(t *testing.T) {
	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeIP,
		RequestsPerSec: 1,
		BurstSize:      1,
		WhitelistPaths: []string{},
	}

	rl := NewAPIRateLimiter(config)
	defer rl.Stop() // Ensure cleanup goroutine is stopped

	// Create some buckets
	c1, _ := gin.CreateTestContext(httptest.NewRecorder())
	c1.Request = httptest.NewRequest("GET", "/", nil)
	c1.Request.RemoteAddr = "192.168.1.100:12345"
	key1 := rl.getKey(c1)
	rl.getBucket(key1, "/")

	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest("GET", "/", nil)
	c2.Request.RemoteAddr = "192.168.1.200:12345"
	key2 := rl.getKey(c2)
	rl.getBucket(key2, "/")

	// Should have 2 buckets
	rl.mutex.RLock()
	assert.Equal(t, 2, len(rl.buckets))
	rl.mutex.RUnlock()

	// Manually set lastRefill to be old for cleanup test
	rl.mutex.Lock()
	for _, bucket := range rl.buckets {
		bucket.mutex.Lock()
		bucket.lastRefill = time.Now().Add(-15 * time.Minute)
		bucket.mutex.Unlock()
	}
	rl.mutex.Unlock()

	// Trigger cleanup
	rl.cleanupExpiredBuckets()

	// Should have 0 buckets after cleanup
	time.Sleep(100 * time.Millisecond) // Give cleanup time to run
	rl.mutex.RLock()
	assert.Equal(t, 0, len(rl.buckets))
	rl.mutex.RUnlock()
}

func TestRateLimitMiddleware_EdgeCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Zero Rate Limit Config", func(t *testing.T) {
		config := RateLimiterConfig{
			Enabled:        true,
			Mode:           ModeIP,
			RequestsPerSec: 0, // Should default to 10
			BurstSize:      0, // Should default to 20
			WhitelistPaths: []string{},
		}

		middleware := RateLimitMiddleware(config)
		router := gin.New()
		router.Use(middleware)
		router.GET("/test", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "ok"})
		})

		// Should work with defaults
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)
	})

	t.Run("Empty Mode", func(t *testing.T) {
		config := RateLimiterConfig{
			Enabled:        true,
			Mode:           "", // Should default to IP
			RequestsPerSec: 1,
			BurstSize:      1,
			WhitelistPaths: []string{},
		}

		middleware := RateLimitMiddleware(config)
		router := gin.New()
		router.Use(middleware)
		router.GET("/test", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "ok"})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)
	})
}

func TestRateLimitMiddleware_PerRouteOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeIP,
		RequestsPerSec: 100, // High default
		BurstSize:      200,
		WhitelistPaths: []string{},
		RouteConfigs: map[string]RouteSpecificConfig{
			"/api/sensitive": {RequestsPerSec: 2, BurstSize: 5}, // Strict override
		},
	}

	middleware := RateLimitMiddleware(config)
	router := gin.New()
	router.Use(middleware)
	router.GET("/api/sensitive", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "sensitive"})
	})
	router.GET("/api/normal", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "normal"})
	})

	t.Run("Sensitive endpoint has strict limits", func(t *testing.T) {
		// Should allow 5 requests (burst) then block
		for i := 0; i < 5; i++ {
			req := httptest.NewRequest("GET", "/api/sensitive", nil)
			req.RemoteAddr = "192.168.1.100:12345"
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, 200, w.Code, "Request %d should succeed", i+1)
		}

		// 6th request should be blocked
		req := httptest.NewRequest("GET", "/api/sensitive", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, 429, w.Code, "6th request should be rate limited")
	})

	t.Run("Normal endpoint has default limits", func(t *testing.T) {
		// Should allow many more requests (200 burst)
		for i := 0; i < 10; i++ {
			req := httptest.NewRequest("GET", "/api/normal", nil)
			req.RemoteAddr = "192.168.1.100:12345"
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, 200, w.Code, "Request %d should succeed", i+1)
		}
	})
}

func TestRateLimitMiddleware_RouteSpecificBuckets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeIP,
		RequestsPerSec: 10,
		BurstSize:      10,
		WhitelistPaths: []string{},
		RouteConfigs: map[string]RouteSpecificConfig{
			"/api/endpoint1": {RequestsPerSec: 2, BurstSize: 2},
			"/api/endpoint2": {RequestsPerSec: 5, BurstSize: 5},
		},
	}

	middleware := RateLimitMiddleware(config)
	router := gin.New()
	router.Use(middleware)
	router.GET("/api/endpoint1", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "endpoint1"})
	})
	router.GET("/api/endpoint2", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "endpoint2"})
	})

	// Exhaust endpoint1
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/endpoint1", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)
	}

	// endpoint1 should be rate limited
	req1 := httptest.NewRequest("GET", "/api/endpoint1", nil)
	req1.RemoteAddr = "192.168.1.100:12345"
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, 429, w1.Code)

	// endpoint2 should still work (separate bucket)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/endpoint2", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code, "Request %d to endpoint2 should succeed", i+1)
	}
}

func TestRateLimitMiddleware_Logging(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:          true,
		Mode:             ModeIP,
		RequestsPerSec:   1,
		BurstSize:        1,
		WhitelistPaths:   []string{},
		LogRateLimitHits: true,
	}

	middleware := RateLimitMiddleware(config)
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	// First request should succeed
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	// Second request should be rate limited (logging is tested implicitly by not panicking)
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.100:12345"
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, 429, w2.Code)
}
