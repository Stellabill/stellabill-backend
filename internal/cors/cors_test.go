package cors_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stellabill-backend/internal/cors"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newRouter(p *cors.Profile) *gin.Engine {
	r := gin.New()
	r.Use(cors.Middleware(p))
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/charge", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func request(r *gin.Engine, method, path, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func preflight(r *gin.Engine, path, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodOptions, path, nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// --- Development profile ---

func TestDev_WildcardOriginAllowed(t *testing.T) {
	r := newRouter(cors.DevelopmentProfile())
	w := request(r, http.MethodGet, "/ping", "http://localhost:3000")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected *, got %q", got)
	}
}

func TestDev_NoCredentialsWithWildcard(t *testing.T) {
	r := newRouter(cors.DevelopmentProfile())
	w := request(r, http.MethodGet, "/ping", "http://localhost:3000")
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got == "true" {
		t.Fatal("credentials must not be set alongside wildcard origin")
	}
}

func TestDev_PreflightReturns204(t *testing.T) {
	r := newRouter(cors.DevelopmentProfile())
	w := preflight(r, "/charge", "http://localhost:3000")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestDev_NoMaxAge(t *testing.T) {
	r := newRouter(cors.DevelopmentProfile())
	w := preflight(r, "/charge", "http://localhost:3000")
	if got := w.Header().Get("Access-Control-Max-Age"); got != "" {
		t.Fatalf("dev profile should not set Max-Age, got %q", got)
	}
}

// --- Production profile ---

func TestProd_AllowedOriginReflected(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "https://app.stellarbill.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.stellarbill.com" {
		t.Fatalf("expected origin reflected, got %q", got)
	}
}

func TestProd_DisallowedOriginNoHeader(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "https://evil.example.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed origin must not receive ACAO header, got %q", got)
	}
}

func TestProd_DisallowedOriginPreflightForbidden(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := preflight(r, "/charge", "https://evil.example.com")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed preflight origin, got %d", w.Code)
	}
}

func TestProd_CredentialsSet(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "https://app.stellarbill.com")
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("expected credentials true, got %q", got)
	}
}

func TestProd_MaxAgeSet(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := preflight(r, "/charge", "https://app.stellarbill.com")
	if got := w.Header().Get("Access-Control-Max-Age"); got == "" {
		t.Fatal("production profile should set Access-Control-Max-Age")
	}
}

func TestProd_VaryHeaderAlwaysSet(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "https://app.stellarbill.com")
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("expected Vary: Origin, got %q", got)
	}
}

// --- Missing / empty origin ---

func TestNoOriginHeader_PassesThrough(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "") // no Origin header
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("no CORS headers expected for same-origin requests, got %q", got)
	}
}

// --- ProfileForEnv ---

func TestProfileForEnv_DevelopmentIsWildcard(t *testing.T) {
	p := cors.ProfileForEnv("development", "")
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "http://localhost:5173")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected wildcard in development, got %q", got)
	}
}

func TestProfileForEnv_ProductionUsesAllowlist(t *testing.T) {
	p := cors.ProfileForEnv("production", "https://app.stellarbill.com, https://admin.stellarbill.com")
	r := newRouter(p)

	w := request(r, http.MethodGet, "/ping", "https://admin.stellarbill.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.stellarbill.com" {
		t.Fatalf("expected admin origin reflected, got %q", got)
	}
}

func TestProfileForEnv_ProductionNoOriginsConfigured_FailsClosed(t *testing.T) {
	p := cors.ProfileForEnv("production", "")
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "https://anything.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no ACAO header when no origins configured, got %q", got)
	}
}

func TestProfileForEnv_StagingUsesAllowlist(t *testing.T) {
	p := cors.ProfileForEnv("staging", "https://staging.stellarbill.com")
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "https://staging.stellarbill.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://staging.stellarbill.com" {
		t.Fatalf("expected staging origin reflected, got %q", got)
	}
}

// --- Multiple allowed origins ---

func TestProd_MultipleOriginsAllowed(t *testing.T) {
	origins := []string{"https://app.stellarbill.com", "https://admin.stellarbill.com"}
	p := cors.ProductionProfile(origins)
	r := newRouter(p)

	for _, origin := range origins {
		w := request(r, http.MethodGet, "/ping", origin)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("expected %q reflected, got %q", origin, got)
		}
	}
}

// --- Custom MaxAge ---

func TestCustomMaxAge(t *testing.T) {
	p := &cors.Profile{
		AllowedOrigins: []string{"https://app.stellarbill.com"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         30 * time.Minute,
	}
	r := newRouter(p)
	w := preflight(r, "/charge", "https://app.stellarbill.com")
	if got := w.Header().Get("Access-Control-Max-Age"); got != "1800" {
		t.Fatalf("expected Max-Age 1800, got %q", got)
	}
}

