package auth_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gopkg.in/yaml.v3"

	"stellarbill-backend/internal/routes"
)

const (
	rbacJWTSecret  = "RBACTest1!JwtSecret"
	rbacAdminToken = "RBACTest1!AdminToken"
	rbacTenantID   = "tenant-rbac"
)

type rbacMatrix struct {
	Roles              []string          `yaml:"roles"`
	DefaultDeniedRoles []string          `yaml:"default_denied_roles"`
	Routes             []rbacRouteMatrix `yaml:"routes"`
}

type rbacRouteMatrix struct {
	Method      string            `yaml:"method"`
	Path        string            `yaml:"path"`
	RequestPath string            `yaml:"request_path"`
	Headers     map[string]string `yaml:"headers"`
	Body        string            `yaml:"body"`
	Roles       map[string]int    `yaml:"roles"`
}

func TestRBACMatrix(t *testing.T) {
	withRBACMatrixEnv(t)

	matrix := loadRBACMatrix(t)
	router := newRBACMatrixRouter(t)

	registeredProtected := protectedRoutes(router)
	matrixRoutes := make(map[string]rbacRouteMatrix, len(matrix.Routes))
	for _, route := range matrix.Routes {
		key := routeKey(route.Method, route.Path)
		if _, exists := matrixRoutes[key]; exists {
			t.Fatalf("duplicate RBAC matrix route %s", key)
		}
		if route.RequestPath == "" {
			t.Fatalf("%s must define request_path", key)
		}
		matrixRoutes[key] = route
	}

	for key := range registeredProtected {
		if _, ok := matrixRoutes[key]; !ok {
			t.Fatalf("protected route %s is missing from internal/auth/rbac_matrix.yaml", key)
		}
	}
	for key := range matrixRoutes {
		if _, ok := registeredProtected[key]; !ok {
			t.Fatalf("RBAC matrix route %s is not registered as a protected route", key)
		}
	}

	for _, route := range matrix.Routes {
		route := route
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			assertRBACStatus(t, router, route, "", http.StatusUnauthorized)

			for _, role := range matrix.Roles {
				expected, ok := route.Roles[role]
				if !ok {
					t.Fatalf("%s %s missing role %q expectation", route.Method, route.Path, role)
				}
				assertRBACStatus(t, router, route, role, expected)
			}

			for _, role := range matrix.DefaultDeniedRoles {
				assertRBACStatus(t, router, route, role, http.StatusForbidden)
			}
		})
	}
}

func loadRBACMatrix(t *testing.T) rbacMatrix {
	t.Helper()

	path := filepath.Join("rbac_matrix.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read RBAC matrix: %v", err)
	}

	var matrix rbacMatrix
	if err := yaml.Unmarshal(data, &matrix); err != nil {
		t.Fatalf("parse RBAC matrix: %v", err)
	}
	if len(matrix.Roles) == 0 {
		t.Fatal("RBAC matrix must define at least one role")
	}
	if len(matrix.DefaultDeniedRoles) == 0 {
		t.Fatal("RBAC matrix must define at least one default-denied role")
	}
	if len(matrix.Routes) == 0 {
		t.Fatal("RBAC matrix must define at least one protected route")
	}
	return matrix
}

func newRBACMatrixRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	cleanup := routes.RegisterWithCleanup(router)
	t.Cleanup(func() {
		if err := cleanup(nil); err != nil {
			t.Fatalf("route cleanup: %v", err)
		}
	})
	return router
}

func protectedRoutes(router *gin.Engine) map[string]struct{} {
	protected := map[string]struct{}{}
	for _, route := range router.Routes() {
		if isPublicRoute(route.Method, route.Path) {
			continue
		}
		if len(route.Path) >= len("/api") && route.Path[:len("/api")] == "/api" {
			protected[routeKey(route.Method, route.Path)] = struct{}{}
		}
	}
	return protected
}

func isPublicRoute(method, path string) bool {
	publicRoutes := map[string]struct{}{
		routeKey(http.MethodGet, "/api/metrics"):   {},
		routeKey(http.MethodGet, "/api/health"):    {},
		routeKey(http.MethodGet, "/api/v1/health"): {},
		routeKey(http.MethodGet, "/api/liveness"):  {},
		routeKey(http.MethodGet, "/api/readiness"): {},
	}
	_, ok := publicRoutes[routeKey(method, path)]
	return ok
}

func assertRBACStatus(t *testing.T, router *gin.Engine, route rbacRouteMatrix, role string, expected int) {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(route.Method, route.RequestPath, bytes.NewBufferString(route.Body))
	if route.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range route.Headers {
		req.Header.Set(key, value)
	}
	if role != "" {
		req.Header.Set("Authorization", "Bearer "+rbacToken(t, role))
		req.Header.Set("X-Tenant-ID", rbacTenantID)
	}

	router.ServeHTTP(rec, req)
	if rec.Code != expected {
		if role == "" {
			role = "anonymous"
		}
		t.Fatalf("%s %s as %s: expected %d, got %d: %s", route.Method, route.Path, role, expected, rec.Code, rec.Body.String())
	}
}

func rbacToken(t *testing.T, role string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":    "rbac-" + role,
		"tenant": rbacTenantID,
		"roles":  []string{role},
		"exp":    time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(rbacJWTSecret))
	if err != nil {
		t.Fatalf("sign RBAC JWT: %v", err)
	}
	return signed
}

func withRBACMatrixEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db?sslmode=disable")
	t.Setenv("DATABASE_REPLICA_URL", "")
	t.Setenv("JWT_SECRET", rbacJWTSecret)
	t.Setenv("ADMIN_TOKEN", rbacAdminToken)
	t.Setenv("TRACING_EXPORTER", "none")
	t.Setenv("RATE_LIMIT_ENABLED", "false")
	t.Setenv("LEGACY_API_SUNSET", "Thu, 31 Dec 2026 23:59:59 GMT")
}

func routeKey(method, path string) string {
	return method + " " + path
}
