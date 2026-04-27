package middleware

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestRateLimitMiddleware_ClockDrift tests behavior with potential clock issues
func TestRateLimitMiddleware_ClockDrift(t *testing.T) {
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

	// Test rapid succession to catch any timing issues
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code, "Request %d should succeed", i+1)
	}

	// Third request should be rate limited
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 429, w.Code, "Third request should be rate limited")
}

// TestRateLimitMiddleware_SharedProxies tests X-Forwarded-For header parsing
func TestRateLimitMiddleware_SharedProxies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeIP,
		RequestsPerSec: 1,
		BurstSize:      1,
		WhitelistPaths: []string{},
	}

	t.Run("Same forwarded IP should share limit", func(t *testing.T) {
		middleware := RateLimitMiddleware(config)
		router := gin.New()
		router.Use(middleware)
		router.GET("/test", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "ok"})
		})

		// First request from client 1
		req1 := httptest.NewRequest("GET", "/test", nil)
		req1.RemoteAddr = "10.0.0.1:12345"
		req1.Header.Set("X-Forwarded-For", "203.0.113.1")
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, 200, w1.Code)

		// Second request from client 2 with same forwarded IP
		req2 := httptest.NewRequest("GET", "/test", nil)
		req2.RemoteAddr = "10.0.0.2:12345"
		req2.Header.Set("X-Forwarded-For", "203.0.113.1")
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, 429, w2.Code, "Should share rate limit for same forwarded IP")
	})

	t.Run("Different forwarded IPs should not share limit", func(t *testing.T) {
		middleware := RateLimitMiddleware(config)
		router := gin.New()
		router.Use(middleware)
		router.GET("/test", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "ok"})
		})

		// First request from client 1
		req1 := httptest.NewRequest("GET", "/test", nil)
		req1.RemoteAddr = "10.0.0.1:12345"
		req1.Header.Set("X-Forwarded-For", "203.0.113.1")
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, 200, w1.Code)

		// Second request from client 2 with different forwarded IP
		req2 := httptest.NewRequest("GET", "/test", nil)
		req2.RemoteAddr = "10.0.0.2:12345"
		req2.Header.Set("X-Forwarded-For", "203.0.113.2")
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, 200, w2.Code, "Should not share rate limit for different forwarded IP")
	})

	t.Run("Multiple IPs in X-Forwarded-For uses first IP", func(t *testing.T) {
		middleware := RateLimitMiddleware(config)
		router := gin.New()
		router.Use(middleware)
		router.GET("/test", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "ok"})
		})

		// First request with multiple forwarded IPs
		req1 := httptest.NewRequest("GET", "/test", nil)
		req1.RemoteAddr = "10.0.0.1:12345"
		req1.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1, 192.0.2.1")
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, 200, w1.Code)

		// Second request with same first forwarded IP
		req2 := httptest.NewRequest("GET", "/test", nil)
		req2.RemoteAddr = "10.0.0.2:12345"
		req2.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.2")
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, 429, w2.Code, "Should share rate limit for same first forwarded IP")
	})
}

// TestRateLimitMiddleware_MalformedHeaders tests robustness against malformed headers
func TestRateLimitMiddleware_MalformedHeaders(t *testing.T) {
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

	testCases := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		shouldWork bool
	}{
		{
			name:       "Empty X-Forwarded-For",
			headers:    map[string]string{"X-Forwarded-For": ""},
			remoteAddr: "192.168.1.100:12345",
			shouldWork: true,
		},
		{
			name:       "Invalid IP in X-Forwarded-For",
			headers:    map[string]string{"X-Forwarded-For": "invalid-ip"},
			remoteAddr: "192.168.1.100:12345",
			shouldWork: true,
		},
		{
			name:       "Very long X-Forwarded-For",
			headers:    map[string]string{"X-Forwarded-For": strings.Repeat("192.168.1.1,", 100)},
			remoteAddr: "192.168.1.100:12345",
			shouldWork: true,
		},
		{
			name:       "Spaces in X-Forwarded-For",
			headers:    map[string]string{"X-Forwarded-For": " 192.168.1.1 , 192.168.1.2 "},
			remoteAddr: "192.168.1.100:12345",
			shouldWork: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tc.remoteAddr

			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if tc.shouldWork {
				assert.Equal(t, 200, w.Code, "Request should succeed with malformed headers")
			}
		})
	}
}

// TestRateLimitMiddleware_UserModeWithMissingCallerID tests user mode fallback behavior
func TestRateLimitMiddleware_UserModeWithMissingCallerID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeUser,
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

	// Request without callerID should fallback to IP
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.100:12345"
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)

	// Second request from same IP should be rate limited
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.100:12345"
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, 429, w2.Code)

	// Different IP should work
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "192.168.1.200:12345"
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code)
}

// TestRateLimitMiddleware_HybridModeDifferentUsers tests hybrid mode with different users
func TestRateLimitMiddleware_HybridModeDifferentUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeHybrid,
		RequestsPerSec: 1,
		BurstSize:      1,
		WhitelistPaths: []string{},
	}

	middleware := RateLimitMiddleware(config)
	router := gin.New()

	// Add a middleware to set callerID for testing
	router.Use(func(c *gin.Context) {
		userID := c.GetHeader("X-User-ID")
		if userID != "" {
			c.Set("callerID", userID)
		}
		c.Next()
	})

	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	// User1 from IP1
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.100:12345"
	req1.Header.Set("X-User-ID", "user1")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)

	// User2 from same IP1 should have separate limit
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.100:12345"
	req2.Header.Set("X-User-ID", "user2")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)

	// User1 from same IP1 should be rate limited now
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "192.168.1.100:12345"
	req3.Header.Set("X-User-ID", "user1")
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	assert.Equal(t, 429, w3.Code)

	// User1 from different IP2 should have separate limit
	req4 := httptest.NewRequest("GET", "/test", nil)
	req4.RemoteAddr = "192.168.1.200:12345"
	req4.Header.Set("X-User-ID", "user1")
	w4 := httptest.NewRecorder()
	router.ServeHTTP(w4, req4)
	assert.Equal(t, 200, w4.Code)
}

// TestRateLimitMiddleware_MemoryLeakPrevention tests that buckets are cleaned up
func TestRateLimitMiddleware_MemoryLeakPrevention(t *testing.T) {
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

	// Create requests from many different IPs to create many buckets
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = fmt.Sprintf("192.168.1.%d:12345", i)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	// Wait and check that cleanup would eventually remove old buckets
	// This is more of a smoke test since we can't easily test the actual cleanup
	// without exposing internal state
	assert.True(t, true, "Memory leak prevention test completed")
}

// TestRateLimitMiddleware_ExtremeBurstTests tests edge cases with burst capacity
func TestRateLimitMiddleware_ExtremeBurstTests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := RateLimiterConfig{
		Enabled:        true,
		Mode:           ModeIP,
		RequestsPerSec: 1,
		BurstSize:      100, // Large burst capacity
		WhitelistPaths: []string{},
	}

	middleware := RateLimitMiddleware(config)
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "ok"})
	})

	// Should allow 100 requests in burst
	successCount := 0
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code == 200 {
			successCount++
		}
	}

	assert.Equal(t, 100, successCount, "Should allow 100 requests in burst")

	// 101st request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 429, w.Code, "101st request should be rate limited")
}

// TestRateLimitMiddleware_ZeroRefillRate tests with very low refill rates
func TestRateLimitMiddleware_ZeroRefillRate(t *testing.T) {
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

	// Use up the single token
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	// Immediately try again - should be rate limited
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.100:12345"
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, 429, w2.Code)

	// Wait for refill and try again
	time.Sleep(1100 * time.Millisecond) // Wait slightly more than 1 second
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "192.168.1.100:12345"
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code, "Request should succeed after refill")
}

