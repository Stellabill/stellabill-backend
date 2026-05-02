package cors_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stellarbill-backend/internal/cors"

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
	// Catch-all for method tests so Gin doesn't 405 before the handler runs.
	r.Any("/any", func(c *gin.Context) { c.Status(http.StatusOK) })
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

// --- Profile Validation Tests ---

func TestProfile_ValidateWildcardWithCredentials(t *testing.T) {
	p := &cors.Profile{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true, // Invalid combination
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected validation error for wildcard + credentials")
	}
}

func TestProfile_ValidateDuplicateOrigins(t *testing.T) {
	p := &cors.Profile{
		AllowedOrigins: []string{
			"https://app.stellarbill.com",
			"https://app.stellarbill.com", // duplicate
		},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected validation error for duplicate origins")
	}
}

func TestProfile_ValidateInvalidOriginFormat(t *testing.T) {
	tests := []struct {
		name   string
		origin string
	}{
		{"missing scheme", "app.stellarbill.com"},
		{"with path", "https://app.stellarbill.com/path"},
		{"with query", "https://app.stellarbill.com?key=value"},
		{"with fragment", "https://app.stellarbill.com#section"},
		{"empty origin", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &cors.Profile{
				AllowedOrigins: []string{tt.origin},
				AllowedMethods: []string{"GET"},
				AllowedHeaders: []string{"Content-Type"},
			}
			if err := p.Validate(); err == nil {
				t.Fatalf("expected validation error for origin %q", tt.origin)
			}
		})
	}
}

func TestProfile_ValidateNilProfile(t *testing.T) {
	var p *cors.Profile
	if err := p.Validate(); err == nil {
		t.Fatal("expected validation error for nil profile")
	}
}

func TestProfile_ValidateValidProfile(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	if err := p.Validate(); err != nil {
		t.Fatalf("expected valid profile, got error: %v", err)
	}
}

func TestProfile_ValidateEmptyOriginsWithCredentials(t *testing.T) {
	// A profile with no origins but AllowCredentials: true is semantically wrong.
	// No ACAO header can ever be emitted, so the credentials flag is misleading.
	p := &cors.Profile{
		AllowedOrigins:   []string{},
		AllowedMethods:   []string{"GET"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
	}
	if err := p.Validate(); err == nil {
		t.Fatal("expected validation error for credentials with empty origins")
	}
}

func TestMiddleware_NilProfilePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Middleware is called with nil Profile")
		}
	}()
	cors.Middleware(nil)
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

func TestProfileForEnv_InvalidOriginFailsClosed(t *testing.T) {
	// Invalid origin format should cause ProfileForEnv to fail closed
	p := cors.ProfileForEnv("production", "not-a-valid-url")
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "https://app.stellarbill.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no ACAO header for invalid config, got %q", got)
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

// --- Malformed Origin Tests ---

func TestMalformedOrigin_MissingScheme(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "app.stellarbill.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no ACAO header for malformed origin, got %q", got)
	}
}

func TestMalformedOrigin_WithPath(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "https://app.stellarbill.com/path")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no ACAO header for origin with path, got %q", got)
	}
}

func TestMalformedOrigin_PreflightForbidden(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := preflight(r, "/charge", "not-a-valid-url")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for malformed preflight origin, got %d", w.Code)
	}
}

// --- Case Sensitivity Tests ---

func TestOrigin_CaseSensitive(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	
	// Different case should not match
	w := request(r, http.MethodGet, "/ping", "https://APP.stellarbill.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no ACAO header for case mismatch, got %q", got)
	}
}

// --- Port Handling Tests ---

func TestOrigin_WithExplicitPort(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com:8443"})
	r := newRouter(p)
	
	w := request(r, http.MethodGet, "/ping", "https://app.stellarbill.com:8443")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.stellarbill.com:8443" {
		t.Fatalf("expected origin with port reflected, got %q", got)
	}
}

func TestOrigin_PortMismatch(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com:8443"})
	r := newRouter(p)
	
	// Different port should not match
	w := request(r, http.MethodGet, "/ping", "https://app.stellarbill.com:9443")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no ACAO header for port mismatch, got %q", got)
	}
}

// --- HTTP Methods Tests ---

func TestProd_AllMethodsAllowed(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)

	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	for _, method := range methods {
		// Use /any so all verbs are registered and Gin doesn't 405.
		w := request(r, method, "/any", "https://app.stellarbill.com")
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.stellarbill.com" {
			t.Fatalf("expected ACAO header for %s method, got %q", method, got)
		}
	}
}

// --- Vary Header Tests ---

func TestVaryHeader_AlwaysSetEvenForDisallowedOrigin(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "https://evil.example.com")
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("expected Vary: Origin even for disallowed origin, got %q", got)
	}
}

func TestVaryHeader_SetForNoOrigin(t *testing.T) {
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "")
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("expected Vary: Origin even with no origin, got %q", got)
	}
}

// --- Preflight-only header tests (Gap 4) ---

func TestProd_SimpleRequestDoesNotIncludeAllowMethods(t *testing.T) {
	// Access-Control-Allow-Methods is a preflight-only header per the Fetch spec.
	// It must not appear on simple GET/POST/etc responses.
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := request(r, http.MethodGet, "/ping", "https://app.stellarbill.com")
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "" {
		t.Fatalf("Access-Control-Allow-Methods must not appear on simple requests, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "" {
		t.Fatalf("Access-Control-Allow-Headers must not appear on simple requests, got %q", got)
	}
}

func TestProd_PreflightIncludesAllowMethods(t *testing.T) {
	// Preflight responses MUST include Access-Control-Allow-Methods.
	p := cors.ProductionProfile([]string{"https://app.stellarbill.com"})
	r := newRouter(p)
	w := preflight(r, "/charge", "https://app.stellarbill.com")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for valid preflight, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("expected Access-Control-Allow-Methods in preflight response")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("expected Access-Control-Allow-Headers in preflight response")
	}
}
