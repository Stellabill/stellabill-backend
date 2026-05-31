package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestTenantRateLimiter_Allow(t *testing.T) {
	limiter := NewTenantRateLimiter(5, 10)
	defer limiter.Stop()

	tests := []struct {
		name      string
		tenantID  string
		requests  int
		expected  int // allowed requests
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
	limiter := NewTenantRateLimiter(10, 20)
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
		RPS:     5,
		Burst:   10,
	}
	middleware := TenantRateLimitMiddleware(config)

	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Test anonymous requests (no tenantID in context)
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		router.ServeHTTP(w, req)

		if i < 10 {
			if w.Code != http.StatusOK {
				t.Errorf("request %d: expected status OK, got %d", i, w.Code)
			}
		} else {
			if w.Code != http.StatusTooManyRequests {
				t.Errorf("request %d: expected status 429, got %d", i, w.Code)
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
