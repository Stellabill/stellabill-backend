package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/routes"
	"stellarbill-backend/openapi"
)

func TestOpenAPI_AllImplementedRoutesInSpec(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	routes.Register(engine)

	doc, err := openapi.Load()
	if err != nil {
		t.Fatalf("load openapi: %v", err)
	}

	specPaths := doc.Paths.Map()
	engineRoutes := engine.Routes()

	for _, r := range engineRoutes {
		if !strings.HasPrefix(r.Path, "/api/") {
			continue
		}
		openAPIPath := ginPathToOpenAPIPath(r.Path)
		item := specPaths[openAPIPath]
		if item == nil {
			t.Fatalf("route missing from openapi spec: %s %s (expected path %q)", r.Method, r.Path, openAPIPath)
		}
		method := strings.ToUpper(r.Method)
		operation := item.GetOperation(method)
		if operation == nil {
			t.Fatalf("route method missing from openapi spec: %s %s", r.Method, r.Path)
		}
	}
}

func TestOpenAPI_AllSpecRoutesImplemented(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	routes.Register(engine)

	doc, err := openapi.Load()
	if err != nil {
		t.Fatalf("load openapi: %v", err)
	}

	engineRoutes := engine.Routes()
	implementedPaths := make(map[string]map[string]bool)
	for _, r := range engineRoutes {
		if !strings.HasPrefix(r.Path, "/api/") {
			continue
		}
		openAPIPath := ginPathToOpenAPIPath(r.Path)
		if implementedPaths[openAPIPath] == nil {
			implementedPaths[openAPIPath] = make(map[string]bool)
		}
		implementedPaths[openAPIPath][strings.ToUpper(r.Method)] = true
	}

	for specPath, pathItem := range doc.Paths.Map() {
		if !strings.HasPrefix(specPath, "/api/") {
			continue
		}
		methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"}
		for _, method := range methods {
			var op *openapi3.Operation
			switch method {
			case "GET":
				op = pathItem.Get
			case "POST":
				op = pathItem.Post
			case "PUT":
				op = pathItem.Put
			case "PATCH":
				op = pathItem.Patch
			case "DELETE":
				op = pathItem.Delete
			case "OPTIONS":
				op = pathItem.Options
			case "HEAD":
				op = pathItem.Head
			}
			if op == nil {
				continue
			}
			if !implementedPaths[specPath][method] {
				t.Fatalf("spec route not implemented: %s %s", method, specPath)
			}
		}
	}
}

func TestOpenAPI_RequestResponseValidation(t *testing.T) {
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

	testCases := buildTestCases()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.url, bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.authRequired {
				req.Header.Set("Authorization", "Bearer test-token")
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

type testCase struct {
	name         string
	method       string
	url          string
	body         []byte
	authRequired bool
}

func buildTestCases() []testCase {
	return []testCase{
		{
			name:         "health",
			method:       http.MethodGet,
			url:          "http://localhost:8080/api/health",
			authRequired: false,
		},
		{
			name:         "plans",
			method:       http.MethodGet,
			url:          "http://localhost:8080/api/v1/plans",
			authRequired: true,
		},
		{
			name:         "subscriptions",
			method:       http.MethodGet,
			url:          "http://localhost:8080/api/v1/subscriptions",
			authRequired: true,
		},
		{
			name:         "subscription_by_id",
			method:       http.MethodGet,
			url:          "http://localhost:8080/api/v1/subscriptions/sub_test",
			authRequired: true,
		},
		{
			name:         "statements",
			method:       http.MethodGet,
			url:          "http://localhost:8080/api/v1/statements",
			authRequired: true,
		},
		{
			name:         "statement_by_id",
			method:       http.MethodGet,
			url:          "http://localhost:8080/api/v1/statements/stmt_test",
			authRequired: true,
		},
		{
			name:         "admin_purge",
			method:       http.MethodPost,
			url:          "http://localhost:8080/api/v1/admin/purge",
			body:         []byte(`{}`),
			authRequired: true,
		},
		{
			name:         "admin_diagnostics",
			method:       http.MethodGet,
			url:          "http://localhost:8080/api/v1/admin/diagnostics",
			authRequired: true,
		},
		{
			name:         "admin_reconcile",
			method:       http.MethodPost,
			url:          "http://localhost:8080/api/v1/admin/reconcile",
			body:         []byte(`{"subscriptions": ["sub_1"]}`),
			authRequired: true,
		},
		{
			name:         "admin_reports",
			method:       http.MethodGet,
			url:          "http://localhost:8080/api/v1/admin/reports",
			authRequired: true,
		},
	}
}

func ginPathToOpenAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") && len(p) > 1 {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}
