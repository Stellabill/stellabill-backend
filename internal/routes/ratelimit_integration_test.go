package routes

import (
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"stellarbill-backend/internal/auth"
)

// helper to reset env between tests
func resetRateLimitEnv() {
	os.Unsetenv("RATE_LIMIT_ENABLED")
	os.Unsetenv("RATE_LIMIT_RPS")
	os.Unsetenv("RATE_LIMIT_BURST")
	os.Unsetenv("RATE_LIMIT_MODE")
	os.Unsetenv("RATE_LIMIT_WHITELIST")
}

const ratelimitJWTSecret = "RatelimitTest1!JwtSecret-MixedAlphaNumeric@123"

func makeRatelimitJWT(t *testing.T, sub string, roles []auth.Role) string {
	claims := jwt.MapClaims{
		"sub":       sub,
		"roles":     roles,
		"exp":       time.Now().Add(time.Hour).Unix(),
		"iat":       time.Now().Unix(),
		"tenant_id": "tenant-1",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(ratelimitJWTSecret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)

	if os.Getenv("DATABASE_URL") == "" {
		os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	}
	if os.Getenv("JWT_SECRET") == "" {
		os.Setenv("JWT_SECRET", ratelimitJWTSecret)
	}
	if os.Getenv("ADMIN_TOKEN") == "" {
		os.Setenv("ADMIN_TOKEN", "RatelimitTest1!AdminToken-MixedAlphaNumeric@123")
	}

	r := gin.New()

	// Pre-populate callerID in the Gin context for rate limiting tests
	r.Use(func(c *gin.Context) {
		if cid := c.GetHeader("X-Caller-ID"); cid != "" {
			c.Set("callerID", cid)
		} else if authHeader := c.GetHeader("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			token, _, err := new(jwt.Parser).ParseUnverified(tokenStr, jwt.MapClaims{})
			if err == nil {
				if claims, ok := token.Claims.(jwt.MapClaims); ok {
					if sub, err := claims.GetSubject(); err == nil && sub != "" {
						c.Set("callerID", sub)
					}
				}
			}
		}
		c.Next()
	})

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
	token := makeRatelimitJWT(t, "user-1", []auth.Role{auth.RoleUser})

	// first 2 requests should pass (burst = 2)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "1.1.1.1:1234"
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Tenant-ID", "tenant-1")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)
	}

	// 3rd request should be blocked
	req := httptest.NewRequest("GET", path, nil)
	req.RemoteAddr = "1.1.1.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Tenant-ID", "tenant-1")
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

	path := "/api/v1/subscriptions"
	token := makeRatelimitJWT(t, "user-1", []auth.Role{auth.RoleUser})

	for i := 0; i < 30; i++ {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "2.2.2.2:1234"
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Tenant-ID", "tenant-1")
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
		token := makeRatelimitJWT(t, "user-1", []auth.Role{auth.RoleUser})

		// IP1 exhausts
		req1 := httptest.NewRequest("GET", path, nil)
		req1.RemoteAddr = "10.0.0.1:1111"
		req1.Header.Set("Authorization", "Bearer "+token)
		req1.Header.Set("X-Tenant-ID", "tenant-1")
		w1 := httptest.NewRecorder()
		r.ServeHTTP(w1, req1)
		assert.Equal(t, 200, w1.Code)

		req1b := httptest.NewRequest("GET", path, nil)
		req1b.RemoteAddr = "10.0.0.1:1111"
		req1b.Header.Set("Authorization", "Bearer "+token)
		req1b.Header.Set("X-Tenant-ID", "tenant-1")
		w1b := httptest.NewRecorder()
		r.ServeHTTP(w1b, req1b)
		assert.Equal(t, 429, w1b.Code)

		// different IP should still work
		req2 := httptest.NewRequest("GET", path, nil)
		req2.RemoteAddr = "10.0.0.2:1111"
		req2.Header.Set("Authorization", "Bearer "+token)
		req2.Header.Set("X-Tenant-ID", "tenant-1")
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

		// user1 token
		token1 := makeRatelimitJWT(t, "user1", []auth.Role{auth.RoleUser})
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "10.0.0.1:1111"
		req.Header.Set("Authorization", "Bearer "+token1)
		req.Header.Set("X-Tenant-ID", "tenant-1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, 200, w.Code)

		// user1 again (same client IP) should be blocked (burst=1)
		req1b := httptest.NewRequest("GET", path, nil)
		req1b.RemoteAddr = "10.0.0.1:1111"
		req1b.Header.Set("Authorization", "Bearer "+token1)
		req1b.Header.Set("X-Tenant-ID", "tenant-1")
		w1b := httptest.NewRecorder()
		r.ServeHTTP(w1b, req1b)
		assert.Equal(t, 429, w1b.Code)

		// user2 should not be affected even on same client IP
		token2 := makeRatelimitJWT(t, "user2", []auth.Role{auth.RoleUser})
		req2 := httptest.NewRequest("GET", path, nil)
		req2.RemoteAddr = "10.0.0.1:1111"
		req2.Header.Set("Authorization", "Bearer "+token2)
		req2.Header.Set("X-Tenant-ID", "tenant-1")
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)
		assert.Equal(t, 200, w2.Code)
	})

	t.Run("Hybrid mode separates user+IP", func(t *testing.T) {
		os.Setenv("RATE_LIMIT_ENABLED", "true")
		os.Setenv("RATE_LIMIT_MODE", "hybrid")
		os.Setenv("RATE_LIMIT_RPS", "1")
		os.Setenv("RATE_LIMIT_BURST", "1")

		r := setupRouter()

		path := "/api/v1/subscriptions"

		// user1 token
		token1 := makeRatelimitJWT(t, "user1", []auth.Role{auth.RoleUser})

		// same user different IP should be separate bucket
		req1 := httptest.NewRequest("GET", path, nil)
		req1.RemoteAddr = "10.0.0.1:1111"
		req1.Header.Set("Authorization", "Bearer "+token1)
		req1.Header.Set("X-Tenant-ID", "tenant-1")
		w1 := httptest.NewRecorder()
		r.ServeHTTP(w1, req1)
		assert.Equal(t, 200, w1.Code)

		// same user again on same IP should be rate limited (burst=1)
		req1b := httptest.NewRequest("GET", path, nil)
		req1b.RemoteAddr = "10.0.0.1:1111"
		req1b.Header.Set("Authorization", "Bearer "+token1)
		req1b.Header.Set("X-Tenant-ID", "tenant-1")
		w1b := httptest.NewRecorder()
		r.ServeHTTP(w1b, req1b)
		assert.Equal(t, 429, w1b.Code)

		// same user on a different IP should be allowed
		req2 := httptest.NewRequest("GET", path, nil)
		req2.RemoteAddr = "10.0.0.2:1111"
		req2.Header.Set("Authorization", "Bearer "+token1)
		req2.Header.Set("X-Tenant-ID", "tenant-1")
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)
		assert.Equal(t, 200, w2.Code)
	})
}

func TestRouter_SustainedLoad_Behavior(t *testing.T) {
	resetRateLimitEnv()

	os.Setenv("RATE_LIMIT_ENABLED", "true")
	os.Setenv("RATE_LIMIT_RPS", "5")
	os.Setenv("RATE_LIMIT_BURST", "5")

	r := setupRouter()

	path := "/api/v1/subscriptions"
	token := makeRatelimitJWT(t, "user-1", []auth.Role{auth.RoleUser})

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
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Tenant-ID", "tenant-1")
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