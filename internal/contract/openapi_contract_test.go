package contract_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/routes"
	"stellarbill-backend/openapi"
)

func TestOpenAPI_ImplementedRoutesAreInSpec(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	routes.Register(engine)

	doc, err := openapi.Load()
	if err != nil {
		t.Fatalf("load openapi: %v", err)
	}

	specPaths := doc.Paths.Map()
	for _, r := range engine.Routes() {
		if !strings.HasPrefix(r.Path, "/api/") {
			continue
		}
		openAPIPath := ginPathToOpenAPIPath(r.Path)
		item := specPaths[openAPIPath]
		if item == nil {
			t.Fatalf("route missing from openapi spec: %s %s (expected path %q)", r.Method, r.Path, openAPIPath)
		}
		if item.GetOperation(strings.ToUpper(r.Method)) == nil {
			t.Fatalf("route method missing from openapi spec: %s %s", r.Method, r.Path)
		}
	}
}

func TestOpenAPI_ResponsesMatchSchemas(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	routes.Register(engine)

	doc, err := openapi.Load()
	if err != nil {
		t.Fatalf("load openapi: %v", err)
	}
	oaRouter, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build openapi router: %v", err)
	}

	for _, tc := range []struct {
		name   string
		method string
		path   string
		role   string
	}{
		{name: "health", method: http.MethodGet, path: "/api/health"},
		{name: "plans", method: http.MethodGet, path: "/api/plans", role: "customer"},
		{name: "subscriptions", method: http.MethodGet, path: "/api/subscriptions", role: "merchant"},
		{name: "subscriptionByID", method: http.MethodGet, path: "/api/subscriptions/sub_test", role: "admin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://localhost:8080"+tc.path, nil)
			if tc.role != "" {
				req.Header.Set("X-Role", tc.role)
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			res := rec.Result()
			bodyBytes := rec.Body.Bytes()

			route, pathParams, err := oaRouter.FindRoute(req)
			if err != nil {
				t.Fatalf("find openapi route: %v", err)
			}

			rvi := &openapi3filter.RequestValidationInput{
				Request:    req,
				PathParams: pathParams,
				Route:      route,
			}
			if err := openapi3filter.ValidateRequest(context.Background(), rvi); err != nil {
				t.Fatalf("request validation failed: %v", err)
			}

			rsp := (&openapi3filter.ResponseValidationInput{
				RequestValidationInput: rvi,
				Status:                 res.StatusCode,
				Header:                 res.Header,
			}).SetBodyBytes(bodyBytes)
			if err := openapi3filter.ValidateResponse(context.Background(), rsp); err != nil {
				t.Fatalf("response validation failed: %v", err)
			}
		})
	}
}

func ginPathToOpenAPIPath(path string) string {
	// Gin uses `:param`, OpenAPI uses `{param}`.
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") && len(p) > 1 {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}
