package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	routeTestJWTSecret  = "RouteTest1!JwtSecret-MixedAlphaNumeric@123"
	routeTestAdminToken = "RouteTest1!AdminToken-MixedAlphaNumeric@123"
)

func TestRegister_SubscriptionDetailAliasesMatchAndDeprecationIsLegacyOnly(t *testing.T) {
	withRouteTestEnv(t)

	router := newRegisteredTestRouter(t)
	token := makeRouteTestJWT(t, "caller-1", "tenant-1", []string{"user"})

	v1Res := performAuthorizedRequest(t, router, http.MethodGet, "/api/v1/subscriptions/sub-123", token)
	legacyRes := performAuthorizedRequest(t, router, http.MethodGet, "/api/subscriptions/sub-123", token)

	if v1Res.Code != http.StatusOK {
		t.Fatalf("v1 route: expected 200, got %d", v1Res.Code)
	}
	if legacyRes.Code != http.StatusOK {
		t.Fatalf("legacy route: expected 200, got %d", legacyRes.Code)
	}
	if got, want := legacyRes.Body.String(), v1Res.Body.String(); got != want {
		t.Fatalf("expected identical subscription detail bodies, got legacy=%s v1=%s", got, want)
	}

	if got := v1Res.Header().Get("Deprecation"); got != "" {
		t.Fatalf("v1 route should not emit Deprecation, got %q", got)
	}
	if got := v1Res.Header().Get("Sunset"); got != "" {
		t.Fatalf("v1 route should not emit Sunset, got %q", got)
	}
	if got := v1Res.Header().Get("Link"); got != "" {
		t.Fatalf("v1 route should not emit Link, got %q", got)
	}

	if got := legacyRes.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("legacy route should emit Deprecation=true, got %q", got)
	}
	if got := legacyRes.Header().Get("Sunset"); got == "" {
		t.Fatal("legacy route should emit Sunset")
	}
	if got, want := legacyRes.Header().Get("Link"), `</api/v1/subscriptions/sub-123>; rel="successor-version"`; got != want {
		t.Fatalf("legacy route should emit successor link %q, got %q", want, got)
	}
}

func TestRegister_SubscriptionDetailAliasesEnforceRBAC(t *testing.T) {
	withRouteTestEnv(t)

	router := newRegisteredTestRouter(t)
	token := makeRouteTestJWT(t, "caller-1", "tenant-1", []string{"customer"})

	for _, path := range []string{"/api/v1/subscriptions/sub-123", "/api/subscriptions/sub-123"} {
		res := performAuthorizedRequest(t, router, http.MethodGet, path, token)
		if res.Code != http.StatusForbidden {
			t.Fatalf("%s: expected 403 for customer role, got %d", path, res.Code)
		}
	}
}

func TestRegister_LegacyProtectedRoutesEmitDeprecationOnUnauthorized(t *testing.T) {
	withRouteTestEnv(t)

	router := newRegisteredTestRouter(t)
	res := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-123", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
	if got := res.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("expected Deprecation=true on legacy 401, got %q", got)
	}
	if got, want := res.Header().Get("Sunset"), "Thu, 31 Dec 2026 23:59:59 GMT"; got != want {
		t.Fatalf("expected Sunset %q on legacy 401, got %q", want, got)
	}
	if got, want := res.Header().Get("Link"), `</api/v1/subscriptions/sub-123>; rel="successor-version"`; got != want {
		t.Fatalf("expected Link %q on legacy 401, got %q", want, got)
	}
}

func TestRegister_StatementAliasesRequirePermission(t *testing.T) {
	withRouteTestEnv(t)

	router := newRegisteredTestRouter(t)
	token := makeRouteTestJWT(t, "caller-1", "tenant-1", []string{"customer"})

	for _, path := range []string{
		"/api/v1/statements?customer_id=caller-2",
		"/api/statements?customer_id=caller-2",
	} {
		res := performAuthorizedRequest(t, router, http.MethodGet, path, token)
		if res.Code != http.StatusForbidden {
			t.Fatalf("%s: expected 403 for customer role, got %d", path, res.Code)
		}
		if path == "/api/statements?customer_id=caller-2" {
			if got := res.Header().Get("Deprecation"); got != "true" {
				t.Fatalf("%s: expected Deprecation=true on legacy 403, got %q", path, got)
			}
			if got, want := res.Header().Get("Sunset"), "Thu, 31 Dec 2026 23:59:59 GMT"; got != want {
				t.Fatalf("%s: expected Sunset %q on legacy 403, got %q", path, want, got)
			}
			if got, want := res.Header().Get("Link"), `</api/v1/statements>; rel="successor-version"`; got != want {
				t.Fatalf("%s: expected Link %q on legacy 403, got %q", path, want, got)
			}
		}
	}
}

func TestRegister_V1PlansRequirePermission(t *testing.T) {
	withRouteTestEnv(t)

	router := newRegisteredTestRouter(t)
	token := makeRouteTestJWT(t, "caller-1", "tenant-1", []string{"customer"})

	res := performAuthorizedRequest(t, router, http.MethodGet, "/api/v1/plans", token)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for customer role on /api/v1/plans, got %d", res.Code)
	}
}

func withRouteTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("JWT_SECRET", routeTestJWTSecret)
	t.Setenv("ADMIN_TOKEN", routeTestAdminToken)
	t.Setenv("TRACING_EXPORTER", "none")
	t.Setenv("LEGACY_API_SUNSET", "Thu, 31 Dec 2026 23:59:59 GMT")
}

func newRegisteredTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router)
	return router
}

func makeRouteTestJWT(t *testing.T, subject, tenant string, roles []string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":    subject,
		"tenant": tenant,
		"roles":  roles,
		"exp":    time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(routeTestJWTSecret))
	if err != nil {
		t.Fatalf("sign test JWT: %v", err)
	}
	return signed
}

func performAuthorizedRequest(t *testing.T, router *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	res := httptest.NewRecorder()
	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Tenant-ID", "tenant-1")
	router.ServeHTTP(res, req)
	return res
}
