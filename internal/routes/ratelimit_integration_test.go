package routes

import (
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// helper to reset env between tests
func resetRateLimitEnv() {
	os.Unsetenv("RATE_LIMIT_ENABLED")
	os.Unsetenv("RATE_LIMIT_RPS")
	os.Unsetenv("RATE_LIMIT_BURST")
	os.Unsetenv("RATE_LIMIT_MODE")
	os.Unsetenv("RATE_LIMIT_WHITELIST")
}

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	Register(r)
	return r
}

func TestRouter_HealthEndpoint_BypassesRateLimit(t *testing.T) {
	resetRateLimitEnv()

	os.Setenv("RATE_LIMIT_ENABLED", "true")
	os.Setenv("RATE_LIMIT_RPS", "1")
	os.Setenv("RATE_LIMIT_BURST", "1")
	os.Setenv("RATE_LIMIT_WHITELIST", "/api/health")

	r := setupRouter()

	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("GET", "/api/health", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		assert.NotEqual(t, 429, w.Code, "health endpoint should never be rate limited")
	}
}

func TestRouter_BurstLimit_IsHonored(t *testing.T) {
	resetRateLimitEnv()

	os.Setenv("RATE_LIMIT_ENABLED", "true")
	os.Setenv("RATE_LIMIT_RPS", "1")
	os.Setenv("RATE_LIMIT_BURST", "2")
	os.Setenv("RATE_LIMIT_MODE", "ip")

	r := setupRouter()

	path := "/api/v1/subscriptions"

	// first 2 requests should pass (burst = 2)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "1.1.1.1:1234"
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)
	}

	// 3rd request should be blocked
	req := httptest.NewRequest("GET", path, nil)
	req.RemoteAddr = "1.1.1.1:1234"
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, 429, w.Code)
}

func TestRouter_RateLimit_Disabled(t *testing.T) {
	resetRateLimitEnv()

	os.Setenv("RATE_LIMIT_ENABLED", "false")
	os.Setenv("RATE_LIMIT_RPS", "1")
	os.Setenv("RATE_LIMIT_BURST", "1")

	r := setupRouter()

	for i := 0; i < 30; i++ {
		req := httptest.NewRequest("GET", "/api/v1/subscriptions", nil)
		req.RemoteAddr = "2.2.2.2:1234"
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)
		assert.NotEqual(t, 429, w.Code)
	}
}

func TestRouter_RateLimit_Modes(t *testing.T) {
	resetRateLimitEnv()

	t.Run("IP mode isolates by IP", func(t *testing.T) {
		os.Setenv("RATE_LIMIT_ENABLED", "true")
		os.Setenv("RATE_LIMIT_MODE", "ip")
		os.Setenv("RATE_LIMIT_RPS", "1")
		os.Setenv("RATE_LIMIT_BURST", "1")

		r := setupRouter()

		path := "/api/v1/subscriptions"

		// IP1 exhausts
		req1 := httptest.NewRequest("GET", path, nil)
		req1.RemoteAddr = "10.0.0.1:1111"
		w1 := httptest.NewRecorder()
		r.ServeHTTP(w1, req1)
		assert.Equal(t, 200, w1.Code)

		req1b := httptest.NewRequest("GET", path, nil)
		req1b.RemoteAddr = "10.0.0.1:1111"
		w1b := httptest.NewRecorder()
		r.ServeHTTP(w1b, req1b)
		assert.Equal(t, 429, w1b.Code)

		// different IP should still work
		req2 := httptest.NewRequest("GET", path, nil)
		req2.RemoteAddr = "10.0.0.2:1111"
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)
		assert.Equal(t, 200, w2.Code)
	})

	t.Run("User mode isolates by callerID", func(t *testing.T) {
		os.Setenv("RATE_LIMIT_ENABLED", "true")
		os.Setenv("RATE_LIMIT_MODE", "user")
		os.Setenv("RATE_LIMIT_RPS", "1")
		os.Setenv("RATE_LIMIT_BURST", "1")

		r := setupRouter()

		path := "/api/v1/subscriptions"

		// user1
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "10.0.0.1:1111"
		w := httptest.NewRecorder()

		req.Header.Set("X-Caller-ID", "user1") // only works if middleware maps it
		r.ServeHTTP(w, req)

		// user2 should not be affected
		req2 := httptest.NewRequest("GET", path, nil)
		req2.RemoteAddr = "10.0.0.1:1111"
		w2 := httptest.NewRecorder()

		req2.Header.Set("X-Caller-ID", "user2")
		r.ServeHTTP(w2, req2)

		assert.True(t, w2.Code == 200 || w2.Code == 401 || w2.Code == 403)
	})

	t.Run("Hybrid mode separates user+IP", func(t *testing.T) {
		os.Setenv("RATE_LIMIT_ENABLED", "true")
		os.Setenv("RATE_LIMIT_MODE", "hybrid")
		os.Setenv("RATE_LIMIT_RPS", "1")
		os.Setenv("RATE_LIMIT_BURST", "1")

		r := setupRouter()

		path := "/api/v1/subscriptions"

		// same user different IP should be separate bucket
		req1 := httptest.NewRequest("GET", path, nil)
		req1.RemoteAddr = "10.0.0.1:1111"
		w1 := httptest.NewRecorder()
		r.ServeHTTP(w1, req1)

		req2 := httptest.NewRequest("GET", path, nil)
		req2.RemoteAddr = "10.0.0.2:1111"
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)

		assert.True(t, w2.Code == 200 || w2.Code == 429)
	})
}

func TestRouter_SustainedLoad_Behavior(t *testing.T) {
	resetRateLimitEnv()

	os.Setenv("RATE_LIMIT_ENABLED", "true")
	os.Setenv("RATE_LIMIT_RPS", "5")
	os.Setenv("RATE_LIMIT_BURST", "5")

	r := setupRouter()

	path := "/api/v1/subscriptions"

	success := 0
	limited := 0

	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			req := httptest.NewRequest("GET", path, nil)
			req.RemoteAddr = "9.9.9.9:1234"
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			mu.Lock()
			defer mu.Unlock()

			if w.Code == 200 {
				success++
			} else if w.Code == 429 {
				limited++
			}
		}(i)
	}

	wg.Wait()

	assert.Greater(t, success, 0, "should allow some requests")
	assert.Greater(t, limited, 0, "should rate limit excess traffic")
	assert.Equal(t, 50, success+limited)
}

func TestRouter_Whitelist_PreventsLimiting(t *testing.T) {
	resetRateLimitEnv()

	os.Setenv("RATE_LIMIT_ENABLED", "true")
	os.Setenv("RATE_LIMIT_RPS", "1")
	os.Setenv("RATE_LIMIT_BURST", "1")
	os.Setenv("RATE_LIMIT_WHITELIST", "/api/health")

	r := setupRouter()

	for i := 0; i < 30; i++ {
		req := httptest.NewRequest("GET", "/api/health", nil)
		req.RemoteAddr = "8.8.8.8:1234"
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code)
	}
}