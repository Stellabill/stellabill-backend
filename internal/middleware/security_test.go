package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
		Env:                   "production",
		SecurityFrameAncestors: "'none'",
		SecurityCSPReportURI:  "/csp-report",
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

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("expected CSP default-src self, got %q", csp)
	}
	if !strings.Contains(csp, "report-uri /csp-report") {
		t.Fatalf("expected report-uri in CSP, got %q", csp)
	}
}

func TestSecurityHeaders_HTMLResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Env:                   "production",
		SecurityFrameAncestors: "'none'",
		SecurityCSPReportURI:  "/csp-report",
	}

	router := gin.New()
	router.Use(SecurityHeaders(cfg))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte("<html><body></body></html>"))
	})

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	router.ServeHTTP(rec, req)

	assertHeader(t, rec, "Content-Security-Policy", rec.Header().Get("Content-Security-Policy"))
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'self' 'nonce-") {
		t.Fatalf("expected HTML response to include nonce-based script-src in CSP, got %q", rec.Header().Get("Content-Security-Policy"))
	}
}

func TestSecurityHeaders_Development(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Env:                   "development",
		SecurityFrameAncestors: "'none'",
		SecurityCSPReportURI:  "/csp-report",
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
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Fatalf("expected HSTS omitted in development, got %q", rec.Header().Get("Strict-Transport-Security"))
	}
}

func TestSecurityHeaders_PreventInsecureFrameOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Env:                   "production",
		SecurityFrameAncestors: "'none'",
		SecurityCSPReportURI:  "/csp-report",
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
}

func TestSecurityHeaders_ProxyLayerConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Env:                   "production",
		SecurityFrameAncestors: "'none'",
		SecurityCSPReportURI:  "/csp-report",
	}

	routerWithProxy := gin.New()
	routerWithProxy.Use(func(c *gin.Context) {
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Strict-Transport-Security", "max-age=60")
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
}

func TestCSPReportHandler_ParsesJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.POST("/csp-report", CSPReportHandler())

	payload := `{"csp-report":{"document-uri":"https://example.com/","violated-directive":"script-src","blocked-uri":"inline"}}`
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/csp-report", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/csp-report")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}
