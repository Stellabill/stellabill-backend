package routes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"stellarbill-backend/internal/auth"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"stellarbill-backend/internal/auth"
)

// helper to reset env between tests

func getAuthToken() string {
	token, _ := createToken("Test1!JwtSecret-MixedAlphaNumeric@123", "user123", []auth.Role{auth.RoleUser}, time.Now().Add(time.Hour))
	return "Bearer " + token
}

func resetRateLimitEnv() {
	os.Unsetenv("RATE_LIMIT_ENABLED")
	os.Unsetenv("RATE_LIMIT_RPS")
	os.Unsetenv("RATE_LIMIT_BURST")
	os.Unsetenv("RATE_LIMIT_MODE")
	os.Unsetenv("RATE_LIMIT_WHITELIST")
}


func newAuthRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", getAuthToken())
	return req
}

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	os.Setenv("DATABASE_URL", "postgres://localhost:5432/test?sslmode=disable")
	os.Setenv("JWT_SECRET", "Test-Secret-Must-Be-Long-And-Complex-123!")
	os.Setenv("ADMIN_TOKEN", "Admin-Token-Must-Be-Long-And-Complex-123!")
	os.Setenv("ENV", "development")
	os.Setenv("TRACING_EXPORTER", "none")

	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("MOCK_DB", "true")
	os.Setenv("JWT_SECRET", "Test1!JwtSecret-MixedAlphaNumeric@123")
	os.Setenv("ADMIN_TOKEN", "Admin1!Token-MixedAlphaNumeric@123")

	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("MOCK_DB", "true")
	os.Setenv("JWT_SECRET", "Test1!JwtSecret-MixedAlphaNumeric@123")
	os.Setenv("ADMIN_TOKEN", "Admin1!Token-MixedAlphaNumeric@123")

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

func rateLimitTestToken(sub string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":    sub,
		"roles":  []auth.Role{auth.RoleUser},
		"tenant": "test-tenant",
		"exp":    time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte("Test-Secret-Must-Be-Long-And-Complex-123!"))
	if err != nil {
		panic(err)
	}
	return signed
}

func serveAuthorizedRateLimit(r *gin.Engine, method, path, remoteAddr, sub string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	req.Header.Set("Authorization", "Bearer "+rateLimitTestToken(sub))
	req.Header.Set("X-Tenant-ID", "test-tenant")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
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
		w := serveAuthorizedRateLimit(r, "GET", path, "1.1.1.1:1234", "rate-user")
		assert.Equal(t, 200, w.Code)
	}

	// 3rd request should be blocked
	w := serveAuthorizedRateLimit(r, "GET", path, "1.1.1.1:1234", "rate-user")
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
		w := serveAuthorizedRateLimit(r, "GET", "/api/v1/subscriptions", "2.2.2.2:1234", "rate-user")
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
		w1 := serveAuthorizedRateLimit(r, "GET", path, "10.0.0.1:1111", "ip-user")
		assert.Equal(t, 200, w1.Code)

		w1b := serveAuthorizedRateLimit(r, "GET", path, "10.0.0.1:1111", "ip-user")
		assert.Equal(t, 429, w1b.Code)

		// different IP should still work
		w2 := serveAuthorizedRateLimit(r, "GET", path, "10.0.0.2:1111", "ip-user")
		assert.Equal(t, 200, w2.Code)
	})

	t.Run("User mode isolates by callerID", func(t *testing.T) {
		os.Setenv("RATE_LIMIT_ENABLED", "true")
		os.Setenv("RATE_LIMIT_MODE", "user")
		os.Setenv("RATE_LIMIT_RPS", "1")
		os.Setenv("RATE_LIMIT_BURST", "1")

		r := setupRouter()

		path := "/api/v1/subscriptions"

		// user1 exhausts their bucket
		serveAuthorizedRateLimit(r, "GET", path, "10.0.0.1:1111", "user1")

		// user2 should not be affected
		w2 := serveAuthorizedRateLimit(r, "GET", path, "10.0.0.1:1111", "user2")
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
		serveAuthorizedRateLimit(r, "GET", path, "10.0.0.1:1111", "hybrid-user")
		w2 := serveAuthorizedRateLimit(r, "GET", path, "10.0.0.2:1111", "hybrid-user")
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

			w := serveAuthorizedRateLimit(r, "GET", path, "9.9.9.9:1234", "load-user")

			mu.Lock()
			defer mu.Unlock()

			if w.Code == 200 {
				success++
			} else if w.Code == 429 {
				limited++
			} else {
				t.Logf("Unexpected status %d: %s", w.Code, w.Body.String())
			}
		}(i)
	}

	wg.Wait()

	// Allow token bucket refill before asserting totals.
	time.Sleep(200 * time.Millisecond)

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