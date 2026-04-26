package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/config"
)

func assertHeader(t *testing.T, rec *httptest.ResponseRecorder, key, expected string) {
	t.Helper()
	actual := rec.Header().Get(key)
	if actual != expected {
		t.Errorf("Expected header %s to be %q, got %q", key, expected, actual)
	}
}

func TestSecurityHeaders_Production(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	cfg := &config.Config{
		Env:                    "production",
		SecurityHSTSMaxAge:     "31536000",
		SecurityFrameOpt:       "DENY",
		SecurityFrameAncestors: "'none'",
	}

	router := gin.New()
	router.Use(SecurityHeaders(cfg))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(rec, req)

	assertHeader(t, rec, "X-Frame-Options", "DENY")
	assertHeader(t, rec, "X-Content-Type-Options", "nosniff")
	assertHeader(t, rec, "Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	assertHeader(t, rec, "Content-Security-Policy", "frame-ancestors 'none'")
}

func TestSecurityHeaders_Development(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	cfg := &config.Config{
		Env:                    "development",
		SecurityHSTSMaxAge:     "31536000",
		SecurityFrameOpt:       "SAMEORIGIN",
		SecurityFrameAncestors: "'self'",
	}

	router := gin.New()
	router.Use(SecurityHeaders(cfg))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(rec, req)

	assertHeader(t, rec, "X-Frame-Options", "SAMEORIGIN")
	assertHeader(t, rec, "X-Content-Type-Options", "nosniff")
	assertHeader(t, rec, "Strict-Transport-Security", "") // Should be omitted
	assertHeader(t, rec, "Content-Security-Policy", "frame-ancestors 'self'")
}

func TestSecurityHeaders_PreventInsecureFrameOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	// ALLOW-FROM is insecure/deprecated, should default to DENY
	cfg := &config.Config{
		Env:                "production",
		SecurityHSTSMaxAge: "31536000",
		SecurityFrameOpt:   "ALLOW-FROM https://evil.com",
	}

	router := gin.New()
	router.Use(SecurityHeaders(cfg))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(rec, req)

	assertHeader(t, rec, "X-Frame-Options", "DENY") // Insecure setting prevented
}

func TestSecurityHeaders_ProxyLayerConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	cfg := &config.Config{
		Env:                    "production",
		SecurityHSTSMaxAge:     "31536000",
		SecurityFrameOpt:       "DENY",
		SecurityFrameAncestors: "'none'",
	}

	routerWithProxy := gin.New()
	routerWithProxy.Use(func(c *gin.Context) {
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Strict-Transport-Security", "max-age=60")
		c.Header("Content-Security-Policy", "frame-ancestors 'self'")
		c.Next()
	})
	routerWithProxy.Use(SecurityHeaders(cfg))
	routerWithProxy.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	routerWithProxy.ServeHTTP(rec, req)

	// Since they were already set, our middleware shouldn't overwrite them
	assertHeader(t, rec, "X-Frame-Options", "SAMEORIGIN")
	assertHeader(t, rec, "Strict-Transport-Security", "max-age=60")
	assertHeader(t, rec, "Content-Security-Policy", "frame-ancestors 'self'")
}

func TestSecurityHeaders_CustomCSP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	cfg := &config.Config{
		Env:                    "production",
		SecurityHSTSMaxAge:     "31536000",
		SecurityFrameOpt:       "DENY",
		SecurityFrameAncestors: "https://trusted.com",
	}

	router := gin.New()
	router.Use(SecurityHeaders(cfg))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(rec, req)

	assertHeader(t, rec, "Content-Security-Policy", "frame-ancestors https://trusted.com")
}
