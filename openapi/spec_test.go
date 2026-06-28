package openapi

import (
	"os"
	"strings"
	"testing"

	"stellarbill-backend/internal/routes"

	"github.com/gin-gonic/gin"
)

func TestLoad(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if doc.Paths == nil || doc.Paths.Len() == 0 {
		t.Fatalf("expected non-empty paths")
	}
	if doc.Paths.Find("/api/health") == nil {
		t.Fatalf("expected /api/health to exist")
	}
	if doc.Paths.Find("/api/subscriptions/{id}") == nil {
		t.Fatalf("expected /api/subscriptions/{id} to exist")
	}
}

func TestRawYAML_NotEmpty(t *testing.T) {
	if len(RawYAML()) == 0 {
		t.Fatalf("expected embedded spec to be non-empty")
	}
}

func TestLoadFromData_InvalidYAML(t *testing.T) {
	if _, err := loadFromData([]byte("openapi: [")); err == nil {
		t.Fatalf("expected error for invalid YAML/OpenAPI")
	}
}

func TestLoadFromData_InvalidOpenAPI(t *testing.T) {
	invalid := []byte("openapi: 3.0.3\ninfo: {}\npaths: {}\n")
	if _, err := loadFromData(invalid); err == nil {
		t.Fatalf("expected validation error for invalid OpenAPI document")
	}
}

var exemptedRoutes = map[string]map[string]string{
	"GET": {
		"/api/liveness":              "Internal Kubernetes liveness check, not part of public API client spec",
		"/api/readiness":             "Internal Kubernetes readiness check, not part of public API client spec",
		"/api/v1/health":             "Internal health endpoint registered under v1, not part of public API",
		"/api/v1/subscriptions":      "Legacy/alias endpoint mapping, primary documented path is /api/subscriptions",
		"/api/v1/subscriptions/{id}": "Legacy/alias endpoint mapping, primary documented path is /api/subscriptions/{id}",
		"/api/plans":                 "Legacy/alias endpoint mapping, primary documented path is /api/v1/plans",
		"/api/statements":            "Legacy/alias endpoint mapping, not yet exposed in public client spec",
		"/api/v1/statements":         "Legacy/alias endpoint mapping, not yet exposed in public client spec",
		"/api/statements/{id}":       "Legacy/alias endpoint mapping, not yet exposed in public client spec",
		"/api/v1/statements/{id}":    "Legacy/alias endpoint mapping, not yet exposed in public client spec",
		"/api/admin/diagnostics":     "Internal diagnostic logs endpoint, requires strict admin tokens",
		"/api/admin/reports":         "Internal reconciliation reports, operational use only",
		"/api/admin/feature-flags":   "Admin feature flags list, operational use only",
		"/api/metrics":               "Prometheus metrics endpoint for monitoring",
	},
	"POST": {
		"/api/subscriptions/{id}/status":    "Legacy status transition endpoint, not yet exposed in public spec",
		"/api/v1/subscriptions/{id}/status": "Status transition endpoint, not yet exposed in public spec",
		"/api/admin/purge":                  "Internal cache clear endpoint, operational use only",
		"/api/admin/reconcile":              "Internal reconciliation trigger, operational use only",
	},
	"PATCH": {
		"/api/admin/feature-flags": "Admin feature flags toggle endpoint, operational use only",
	},
}

// TestSpecCoverageMissingPathsDocumented verifies that all registered routes
// have corresponding documentation in the OpenAPI spec.
func TestSpecCoverageMissingPathsDocumented(t *testing.T) {
	// Load the OpenAPI spec
	doc, err := Load()
	if err != nil {
		t.Fatalf("failed to load OpenAPI spec: %v", err)
	}

	// Set required env vars so config validation passes
	if os.Getenv("DATABASE_URL") == "" {
		os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	}
	if os.Getenv("JWT_SECRET") == "" {
		os.Setenv("JWT_SECRET", "Test1!JwtSecret-MixedAlphaNumeric@123")
	}
	if os.Getenv("ADMIN_TOKEN") == "" {
		os.Setenv("ADMIN_TOKEN", "Admin1!Token-MixedAlphaNumeric@123")
	}

	// Register all routes
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	routes.Register(engine)
	registeredRoutes := engine.Routes()

	// Build a map of documented paths and methods from OpenAPI spec
	specPaths := make(map[string]map[string]bool) // path -> method -> bool
	for path, pathItem := range doc.Paths.Map() {
		if !strings.HasPrefix(path, "/api/") {
			continue
		}
		specPaths[path] = make(map[string]bool)
		if pathItem.Get != nil {
			specPaths[path]["GET"] = true
		}
		if pathItem.Post != nil {
			specPaths[path]["POST"] = true
		}
		if pathItem.Put != nil {
			specPaths[path]["PUT"] = true
		}
		if pathItem.Patch != nil {
			specPaths[path]["PATCH"] = true
		}
		if pathItem.Delete != nil {
			specPaths[path]["DELETE"] = true
		}
		if pathItem.Head != nil {
			specPaths[path]["HEAD"] = true
		}
	}

	// Check that all registered routes are documented
	missingPathCount := 0
	for _, r := range registeredRoutes {
		if !strings.HasPrefix(r.Path, "/api/") {
			continue
		}

		// Skip internal/admin routes that aren't documented in public spec
		if strings.HasPrefix(r.Path, "/api/admin") || strings.HasPrefix(r.Path, "/api/metrics") {
			continue
		}

		// Convert gin path to OpenAPI path format
		openAPIPath := ginPathToOpenAPIPath(r.Path)

		// Skip exempted routes
		if _, ok := exemptedRoutes[r.Method][openAPIPath]; ok {
			continue
		}

		// Check if this path and method exist in spec
		if specPaths[openAPIPath] == nil {
			t.Logf("WARN: Route %s %q not in OpenAPI spec", r.Method, openAPIPath)
			missingPathCount++
			continue
		}
		if !specPaths[openAPIPath][r.Method] {
			t.Logf("WARN: Method %s for path %q not in OpenAPI spec", r.Method, openAPIPath)
			missingPathCount++
		}
	}

	if missingPathCount > 0 {
		t.Errorf("found %d routes missing from OpenAPI spec; see above for details", missingPathCount)
	}
}

// ginPathToOpenAPIPath converts Gin path parameters to OpenAPI format.
// E.g., /api/subscriptions/:id -> /api/subscriptions/{id}
func ginPathToOpenAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") && len(p) > 1 {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}
