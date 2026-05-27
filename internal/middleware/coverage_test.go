package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/config"
)

func TestCoverage_AuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mw := AuthMiddleware(nil, "")
	r := gin.New()
	r.Use(mw)
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCoverage_DeprecationHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(DeprecationHeaders())
	r.GET("/api/foo", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/foo", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Deprecation") != "true" {
		t.Fatal("missing deprecation header")
	}
}

func TestCoverage_RequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID())
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCoverage_RequestLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestLogger())
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCoverage_RequestID_Variants(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID())
	r.GET("/", func(c *gin.Context) {
		_ = GetRequestID(c)
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "valid-id-123")
	r.ServeHTTP(rec, req)

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-Request-ID", "this-is-way-too-long-to-pass-the-32-char-limit-and-should-be-rejected")
	r.ServeHTTP(rec2, req2)
}

func TestCoverage_APIRateLimiter_CleanupAndWhitelist(t *testing.T) {
	rl := NewAPIRateLimiter(RateLimiterConfig{
		Enabled:        true,
		WhitelistPaths: []string{"/skip"},
		RequestsPerSec: 1,
		BurstSize:      1,
	})

	if !rl.isWhitelisted("/skip") {
		t.Fatal("expected whitelisted true")
	}
	if rl.isWhitelisted("/other") {
		t.Fatal("expected whitelisted false")
	}
	_ = rl.getBucket("k1", "/path")
}

func TestCoverage_RateLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mw := RateLimitMiddleware(RateLimiterConfig{
		Enabled:        true,
		RequestsPerSec: 1000,
		BurstSize:      100,
	})
	r := gin.New()
	r.Use(mw)
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(rec, req)
}

func TestCoverage_RecoveryLogger(t *testing.T) {
	mw := RecoveryLogger()
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
}

func TestCoverage_WantsPlainText(t *testing.T) {
	if wantsPlainText("") {
		t.Fatal("empty should be false")
	}
	if !wantsPlainText("text/plain") {
		t.Fatal("text/plain should be true")
	}
	if wantsPlainText("application/json") {
		t.Fatal("application/json should be false")
	}
	if wantsPlainText("foo/bar, application/json") {
		t.Fatal("json should win over other")
	}
	if wantsPlainText("foo/bar") {
		t.Fatal("unknown should default to false")
	}
}

func TestCoverage_SafePath(t *testing.T) {
	if safePath(nil) != "" {
		t.Fatal("nil context should return empty")
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if safePath(c) != "" {
		t.Fatal("nil request should return empty")
	}
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest(http.MethodGet, "/foo/bar", nil)
	if safePath(c2) != "/foo/bar" {
		t.Fatal("unexpected path")
	}
}

func TestCoverage_SecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders(&config.Config{}))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
