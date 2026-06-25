package routes

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/openapi"
)

// exemptedRoutes maps HTTP Method to a map of normalized paths to the documented reason
// why they are intentionally excluded from the public OpenAPI specification.
var exemptedRoutes = map[string]map[string]string{
	"GET": {
		"/api/liveness":              "Internal Kubernetes liveness check, not part of public API client spec",
		"/api/readiness":             "Internal Kubernetes readiness check, not part of public API client spec",
		"/api/v1/health":             "Internal health endpoint registered under v1, not part of public API",
		"/api/v1/subscriptions":      "Legacy/alias endpoint mapping, primary documented path is /api/subscriptions",
		"/api/v1/subscriptions/{id}": "Legacy/alias endpoint mapping, primary documented path is /api/subscriptions/{id}",
		"/api/plans":              "Legacy/alias endpoint mapping, primary documented path is /api/v1/plans",
		"/api/statements":         "Legacy/alias endpoint mapping, not yet exposed in public client spec",
		"/api/v1/statements":      "Legacy/alias endpoint mapping, not yet exposed in public client spec",
		"/api/statements/{id}":    "Legacy/alias endpoint mapping, not yet exposed in public client spec",
		"/api/v1/statements/{id}": "Legacy/alias endpoint mapping, not yet exposed in public client spec",
		"/api/admin/diagnostics":  "Internal diagnostic logs endpoint, requires strict admin tokens",
		"/api/admin/reports":      "Internal reconciliation reports, operational use only",
		"/api/admin/feature-flags": "Internal feature flags endpoint",
		"/api/metrics":            "Metrics endpoint",
	},
	"POST": {
		"/api/subscriptions/{id}/status":    "Legacy status transition endpoint, not yet exposed in public spec",
		"/api/v1/subscriptions/{id}/status": "Status transition endpoint, not yet exposed in public spec",
		"/api/admin/purge":                  "Internal cache clear endpoint, operational use only",
		"/api/admin/reconcile":              "Internal reconciliation trigger, operational use only",
	},
	"PATCH": {
		"/api/admin/feature-flags": "Internal feature flags endpoint",
	},
}

// route represents a method and path definition for testing comparison behavior.
type route struct {
	Method string
	Path   string
}

// normalizePath converts a Gin-style parameterized path (e.g. /:id) into
// an OpenAPI-style parameterized path (e.g. /{id}), handles trailing slashes,
// and trims spaces to ensure deterministic parity checks.
func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
	}

	var ginParamRegex = regexp.MustCompile(`/:([a-zA-Z0-9_]+)`)
	return ginParamRegex.ReplaceAllString(path, "/{$1}")
}

// checkParity compares a list of routes against a map of defined OpenAPI paths,
// taking into account exempted routes. Returns a slice of error messages for missing paths.
func checkParity(routes []route, specPaths map[string]bool, exemptions map[string]map[string]string) []string {
	var missing []string
	for _, r := range routes {
		method := strings.ToUpper(r.Method)
		path := normalizePath(r.Path)

		if _, ok := exemptions[method][path]; ok {
			continue
		}

		if !specPaths[path] {
			missing = append(missing, fmt.Sprintf("- Missing path: %s %q", method, path))
		}
	}
	return missing
}

// TestRouteOpenAPIParity ensures that all registered Gin routes (excluding documented exceptions)
// are properly represented in the OpenAPI spec.
func TestRouteOpenAPIParity(t *testing.T) {
	// Set mock environment variables so Register passes config load
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("MOCK_DB", "true")
	os.Setenv("JWT_SECRET", "Test1!JwtSecret-MixedAlphaNumeric@123")
	os.Setenv("ADMIN_TOKEN", "Admin1!Token-MixedAlphaNumeric@123")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("JWT_SECRET")
	defer os.Unsetenv("ADMIN_TOKEN")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r)

	// Load OpenAPI spec
	spec, err := openapi.Load()
	if err != nil {
		t.Fatalf("Failed to load OpenAPI spec: %v", err)
	}

	specPaths := spec.Paths.Map()

	var registeredRoutes []route
	for _, gr := range r.Routes() {
		registeredRoutes = append(registeredRoutes, route{
			Method: gr.Method,
			Path:   gr.Path,
		})
	}

	specPathSet := make(map[string]bool)
	for p, item := range specPaths {
		normalizedSpecPath := normalizePath(p)
		specPathSet[normalizedSpecPath] = true

		// Check method definitions within the PathItem
		methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
		for _, m := range methods {
			op := item.GetOperation(m)
			if op != nil {
				// We map specific method-path combos if we want to be exact,
				// but checkParity does it at a path level. Let's make specPathSet method-specific.
				specPathSet[m+":"+normalizedSpecPath] = true
			}
		}
	}

	// Run exact method + path parity comparison
	var missingRoutes []string
	for _, rt := range registeredRoutes {
		method := strings.ToUpper(rt.Method)
		path := normalizePath(rt.Path)

		// Check for method + path combo exemptions
		if reason, ok := exemptedRoutes[method][path]; ok {
			t.Logf("INFO: Route %s %q is exempted from parity check. Reason: %s", method, path, reason)
			continue
		}

		key := method + ":" + path
		if !specPathSet[key] {
			missingRoutes = append(missingRoutes, fmt.Sprintf("- Missing route: %s %q", method, path))
		}
	}

	if len(missingRoutes) > 0 {
		t.Errorf("Route-to-OpenAPI parity check failed. The following registered routes are missing from the OpenAPI spec:\n%s", strings.Join(missingRoutes, "\n"))
	}
}

// TestNormalizePath tests parameter conversion and trailing slash removal logic.
func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"standard path", "/api/health", "/api/health"},
		{"trailing slash", "/api/health/", "/api/health"},
		{"single parameter", "/api/subscriptions/:id", "/api/subscriptions/{id}"},
		{"multiple parameters", "/api/subscriptions/:id/status/:status_id", "/api/subscriptions/{id}/status/{status_id}"},
		{"mixed case param", "/api/v1/statements/:statement_ID", "/api/v1/statements/{statement_ID}"},
		{"root path", "/", "/"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizePath(tc.input)
			if got != tc.expected {
				t.Errorf("normalizePath(%q) = %q, expected %q", tc.input, got, tc.expected)
			}
		})
	}
}

// TestCheckParity tests the helper logic including path matching and exemption filtering.
func TestCheckParity(t *testing.T) {
	exemptions := map[string]map[string]string{
		"GET": {
			"/api/exempt": "test exemption",
		},
	}

	tests := []struct {
		name        string
		routes      []route
		specPaths   map[string]bool
		expectedLen int
	}{
		{
			name: "exact match",
			routes: []route{
				{"GET", "/api/health"},
			},
			specPaths: map[string]bool{
				"/api/health": true,
			},
			expectedLen: 0,
		},
		{
			name: "missing route",
			routes: []route{
				{"GET", "/api/missing"},
			},
			specPaths:   map[string]bool{},
			expectedLen: 1,
		},
		{
			name: "exempted route",
			routes: []route{
				{"GET", "/api/exempt"},
			},
			specPaths:   map[string]bool{},
			expectedLen: 0,
		},
		{
			name: "normalization match",
			routes: []route{
				{"GET", "/api/subscriptions/:id/"},
			},
			specPaths: map[string]bool{
				"/api/subscriptions/{id}": true,
			},
			expectedLen: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := checkParity(tc.routes, tc.specPaths, exemptions)
			if len(got) != tc.expectedLen {
				t.Errorf("checkParity got %d missing routes, expected %d. got: %v", len(got), tc.expectedLen, got)
			}
		})
	}
}
