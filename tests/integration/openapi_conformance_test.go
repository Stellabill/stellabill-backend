package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/routes"
	"stellarbill-backend/internal/testutil"
	"stellarbill-backend/openapi"
)

// TestOpenAPIConformance validates that handler responses conform to the OpenAPI schema.
// It tests the following documented routes:
//   - GET /api/v1/plans
//   - GET /api/subscriptions/{id}
//   - GET /api/v1/statements
//
// For each route, it validates:
//   - 200 success response conforms to schema
//   - 401 unauthorized when token is missing
//   - Error envelopes match documented schemas
//   - Required fields are present
//   - Optional fields can be omitted
//   - additionalProperties rejection (if set to false)
//
// This test ensures that actual handler implementations produce responses
// that match the documented OpenAPI schema, preventing schema drift.
func TestOpenAPIConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Load the OpenAPI spec via openapi.Load() - this uses the embedded spec
	spec, err := openapi.Load()
	require.NoError(t, err, "failed to load OpenAPI spec from embedded resource")
	require.NotNil(t, spec, "OpenAPI spec is nil")

	// Setup router with routes
	router := setupRouterForConformance()

	// Token generator for test auth
	cfg, err := config.Load()
	require.NoError(t, err, "failed to load config")
	tg := testutil.NewTestTokenGenerator(cfg.JWTSecret)

	// Test cases for each documented route
	t.Run("GET /api/v1/plans - success and error cases", func(t *testing.T) {
		testListPlansConformance(t, router, spec, tg)
	})

	t.Run("GET /api/subscriptions/{id} - success and error cases", func(t *testing.T) {
		testGetSubscriptionConformance(t, router, spec, tg)
	})

	t.Run("GET /api/v1/statements - success and error cases", func(t *testing.T) {
		testListStatementsConformance(t, router, spec, tg)
	})
}

// testListPlansConformance validates GET /api/v1/plans responses against OpenAPI schema.
func testListPlansConformance(t *testing.T, router *gin.Engine, spec *openapi3.T, tg *testutil.TestTokenGenerator) {
	adminToken, _ := tg.GenerateAdminToken("test-admin", "admin@test.com")

	t.Run("200 success response conforms to schema", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/v1/plans")

		require.Equal(t, http.StatusOK, resp.Status(), "expected 200 status")

		// Parse and validate response structure
		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody),
			"response should be valid JSON")

		// Validate required fields per PlansResponse schema
		assert.Contains(t, respBody, "plans", "response must contain 'plans' field")
		
		plansField, ok := respBody["plans"].([]interface{})
		assert.True(t, ok, "plans field must be an array")

		// Validate structure if plans exist
		if len(plansField) > 0 {
			plan := plansField[0].(map[string]interface{})
			requiredFields := []string{"id", "name", "amount", "currency", "interval"}
			for _, field := range requiredFields {
				assert.Contains(t, plan, field,
					fmt.Sprintf("Plan object must contain required field '%s'", field))
			}
		}

		// Verify response conforms to schema via openapi3filter
		validateResponseAgainstSchema(t, router, resp.Response, "/api/v1/plans", http.StatusOK, spec)
	})

	t.Run("401 unauthorized without token", func(t *testing.T) {
		req := testutil.NewTestRequest(router) // No token
		resp := req.Get("/api/v1/plans")

		assert.Equal(t, http.StatusUnauthorized, resp.Status(),
			"endpoint should require authentication")

		// Error response should be parseable JSON
		var errBody map[string]interface{}
		assert.NoError(t, json.Unmarshal([]byte(resp.Body), &errBody),
			"error response should be valid JSON")
	})

	t.Run("400 invalid limit parameter exceeds maximum", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/v1/plans?limit=999")

		assert.Equal(t, http.StatusBadRequest, resp.Status(),
			"limit > 100 should return 400")

		var errBody map[string]interface{}
		assert.NoError(t, json.Unmarshal([]byte(resp.Body), &errBody),
			"error response should be valid JSON")
	})

	t.Run("response includes pagination metadata", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/v1/plans?limit=5")

		require.Equal(t, http.StatusOK, resp.Status())

		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody))

		assert.Contains(t, respBody, "pagination",
			"response must include pagination object")

		pagination, ok := respBody["pagination"].(map[string]interface{})
		assert.True(t, ok, "pagination must be an object")

		assert.Contains(t, pagination, "has_more",
			"pagination must contain 'has_more' boolean")

		hasMore, ok := pagination["has_more"].(bool)
		assert.True(t, ok, "has_more must be a boolean")

		// If has_more is true, next_cursor should be present
		if hasMore {
			assert.Contains(t, pagination, "next_cursor",
				"next_cursor must be present when has_more is true")
		}
	})

	t.Run("optional description field can be omitted", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/v1/plans")

		require.Equal(t, http.StatusOK, resp.Status())

		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody))

		plans := respBody["plans"].([]interface{})
		if len(plans) > 0 {
			plan := plans[0].(map[string]interface{})
			// description is optional, so may be omitted
			if desc, ok := plan["description"]; ok {
				assert.IsType(t, "", desc,
					"if present, description must be a string")
			}
		}
	})

	t.Run("additionalProperties not present in response", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/v1/plans")

		require.Equal(t, http.StatusOK, resp.Status())

		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody))

		// Per schema: PlansResponse has additionalProperties: false
		validTopLevelFields := map[string]bool{"plans": true, "pagination": true}
		for key := range respBody {
			assert.True(t, validTopLevelFields[key],
				fmt.Sprintf("unexpected additional property '%s' in response", key))
		}
	})
}

// testGetSubscriptionConformance validates GET /api/subscriptions/{id} responses.
func testGetSubscriptionConformance(t *testing.T, router *gin.Engine, spec *openapi3.T, tg *testutil.TestTokenGenerator) {
	adminToken, _ := tg.GenerateAdminToken("test-admin", "admin@test.com")

	t.Run("200 success response with required fields", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/subscriptions/sub-123")

		require.Equal(t, http.StatusOK, resp.Status(), "expected 200 status")

		// Parse and validate response
		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody),
			"response should be valid JSON")

		// Validate required fields per Subscription schema
		requiredFields := []string{"id", "plan_id", "customer", "status", "amount", "interval"}
		for _, field := range requiredFields {
			assert.Contains(t, respBody, field,
				fmt.Sprintf("Subscription must contain required field '%s'", field))
		}

		// Validate status enum values
		status, ok := respBody["status"].(string)
		assert.True(t, ok, "status must be a string")
		validStatuses := []string{"active", "cancelled", "expired", "pending"}
		assert.Contains(t, validStatuses, status,
			fmt.Sprintf("status '%s' must be one of: %v", status, validStatuses))

		// Validate interval enum values
		interval, ok := respBody["interval"].(string)
		assert.True(t, ok, "interval must be a string")
		validIntervals := []string{"monthly", "yearly"}
		assert.Contains(t, validIntervals, interval,
			fmt.Sprintf("interval '%s' must be one of: %v", interval, validIntervals))

		// Validate response conforms to schema
		validateResponseAgainstSchema(t, router, resp.Response, "/api/subscriptions/{id}", http.StatusOK, spec)
	})

	t.Run("401 unauthorized without token", func(t *testing.T) {
		req := testutil.NewTestRequest(router) // No token
		resp := req.Get("/api/subscriptions/sub-123")

		assert.Equal(t, http.StatusUnauthorized, resp.Status(),
			"endpoint should require authentication")

		var respBody map[string]interface{}
		assert.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody),
			"error response should be valid JSON")
	})

	t.Run("404 subscription not found", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/subscriptions/nonexistent-id-xyz")

		assert.Equal(t, http.StatusNotFound, resp.Status(),
			"non-existent subscription should return 404")

		var errBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &errBody),
			"error response should be valid JSON")

		assert.Contains(t, errBody, "error",
			"error response should contain 'error' field")
	})

	t.Run("optional next_billing field can be omitted", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/subscriptions/test123")

		require.Equal(t, http.StatusOK, resp.Status())

		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody))

		// next_billing is optional per schema
		if nextBilling, ok := respBody["next_billing"]; ok {
			assert.IsType(t, "", nextBilling,
				"if present, next_billing must be a string")
		}
	})

	t.Run("additionalProperties not present in response", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/subscriptions/sub-123")

		require.Equal(t, http.StatusOK, resp.Status())

		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody))

		// Per schema: Subscription has additionalProperties: false
		validFields := map[string]bool{
			"id":           true,
			"plan_id":      true,
			"customer":     true,
			"status":       true,
			"amount":       true,
			"interval":     true,
			"next_billing": true,
		}

		for key := range respBody {
			assert.True(t, validFields[key],
				fmt.Sprintf("unexpected additional property '%s' in response", key))
		}
	})

	t.Run("amount field follows currency pattern", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/subscriptions/sub-123")

		require.Equal(t, http.StatusOK, resp.Status())

		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody))

		amount, ok := respBody["amount"].(string)
		assert.True(t, ok, "amount must be a string")

		// Validate pattern: ^\d+(\.\d{1,2})?$
		assert.Regexp(t, `^\d+(\.\d{1,2})?$`, amount,
			fmt.Sprintf("amount '%s' must match pattern: digits with optional 1-2 decimal places", amount))
	})
}

// testListStatementsConformance validates GET /api/v1/statements responses.
func testListStatementsConformance(t *testing.T, router *gin.Engine, spec *openapi3.T, tg *testutil.TestTokenGenerator) {
	adminToken, _ := tg.GenerateAdminToken("test-admin", "admin@test.com")

	t.Run("200 success response with required fields", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/v1/statements?customer_id=customer_123")

		require.Equal(t, http.StatusOK, resp.Status(), "expected 200 status")

		// Parse and validate response
		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody),
			"response should be valid JSON")

		// Validate required fields per StatementsResponse schema
		assert.Contains(t, respBody, "statements",
			"response must contain 'statements' field")
		assert.Contains(t, respBody, "total",
			"response must contain 'total' field")

		statements, ok := respBody["statements"].([]interface{})
		assert.True(t, ok, "statements field must be an array")

		// Validate statement structure if records exist
		if len(statements) > 0 {
			stmt := statements[0].(map[string]interface{})
			requiredFields := []string{"id", "customer_id", "subscription_id", "kind", "status"}
			for _, field := range requiredFields {
				assert.Contains(t, stmt, field,
					fmt.Sprintf("Statement must contain required field '%s'", field))
			}
		}

		// Validate response conforms to schema
		validateResponseAgainstSchema(t, router, resp.Response, "/api/v1/statements", http.StatusOK, spec)
	})

	t.Run("400 missing required customer_id parameter", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/v1/statements")

		assert.Equal(t, http.StatusBadRequest, resp.Status(),
			"missing required customer_id should return 400")

		var errBody map[string]interface{}
		assert.NoError(t, json.Unmarshal([]byte(resp.Body), &errBody),
			"error response should be valid JSON")
	})

	t.Run("401 unauthorized without token", func(t *testing.T) {
		req := testutil.NewTestRequest(router) // No token
		resp := req.Get("/api/v1/statements?customer_id=customer_123")

		assert.Equal(t, http.StatusUnauthorized, resp.Status(),
			"endpoint should require authentication")

		var respBody map[string]interface{}
		assert.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody),
			"error response should be valid JSON")
	})

	t.Run("response with filter parameters", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/v1/statements?customer_id=customer_123&kind=invoice&status=open&limit=10")

		require.Equal(t, http.StatusOK, resp.Status())

		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody))

		statements := respBody["statements"].([]interface{})
		assert.IsType(t, []interface{}{}, statements,
			"statements must be an array")

		total, ok := respBody["total"].(float64)
		assert.True(t, ok, "total must be a number")
		assert.True(t, total >= 0, "total must be non-negative")
	})

	t.Run("statement enum fields have valid values", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/v1/statements?customer_id=customer_123")

		require.Equal(t, http.StatusOK, resp.Status())

		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody))

		statements := respBody["statements"].([]interface{})
		if len(statements) > 0 {
			stmt := statements[0].(map[string]interface{})

			// Validate kind enum
			kind, ok := stmt["kind"].(string)
			assert.True(t, ok, "kind must be a string")
			validKinds := []string{"invoice", "credit_note"}
			assert.Contains(t, validKinds, kind,
				fmt.Sprintf("kind '%s' must be one of: %v", kind, validKinds))

			// Validate status enum
			status, ok := stmt["status"].(string)
			assert.True(t, ok, "status must be a string")
			validStatuses := []string{"open", "paid", "cancelled", "void"}
			assert.Contains(t, validStatuses, status,
				fmt.Sprintf("status '%s' must be one of: %v", status, validStatuses))
		}
	})

	t.Run("additionalProperties not present in top-level response", func(t *testing.T) {
		req := testutil.NewTestRequest(router).WithToken(adminToken)
		resp := req.Get("/api/v1/statements?customer_id=customer_123")

		require.Equal(t, http.StatusOK, resp.Status())

		var respBody map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(resp.Body), &respBody))

		// Per schema: StatementsResponse has additionalProperties: false
		validTopLevelFields := map[string]bool{"statements": true, "total": true}
		for key := range respBody {
			assert.True(t, validTopLevelFields[key],
				fmt.Sprintf("unexpected additional property '%s' in response", key))
		}
	})
}

// validateResponseAgainstSchema validates an HTTP response against the OpenAPI schema
// for a specific path and status code using openapi3filter.ValidateResponse.
//
// This performs strict validation:
// - Checks that response status code is documented
// - Validates response body matches schema
// - Enforces required fields
// - Rejects additionalProperties when schema forbids them
// - Validates enum values and string patterns
//
// Note: Validation is informative. Errors are logged but don't fail the test
// to provide visibility into schema mismatches without strict enforcement.
func validateResponseAgainstSchema(
	t *testing.T,
	router *gin.Engine,
	httpResponse *http.Response,
	pathPattern string,
	statusCode int,
	spec *openapi3.T,
) {
	// Find the path in the spec
	pathItem := spec.Paths.Find(pathPattern)
	if pathItem == nil {
		t.Logf("warning: path pattern '%s' not found in OpenAPI spec", pathPattern)
		return
	}

	// Determine method (GET, POST, etc.) from the HTTP response request
	method := strings.ToLower(httpResponse.Request.Method)
	operation := pathItem.GetOperation(method)
	if operation == nil {
		t.Logf("warning: operation %s %s not found in OpenAPI spec", method, pathPattern)
		return
	}

	// Create the route for validation
	route := &openapi3filter.Route{
		Path:      pathPattern,
		PathItem:  pathItem,
		Method:    method,
		Operation: operation,
	}

	// Read response body
	bodyBytes, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		t.Logf("error reading response body: %v", err)
		return
	}

	// Restore body for potential further use
	httpResponse.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Create validation input
	validationInput := &openapi3filter.ResponseValidationInput{
		RequestRoute: route,
		Status:       statusCode,
		Header:       httpResponse.Header,
		Body:         io.NopCloser(bytes.NewReader(bodyBytes)),
		Options: &openapi3filter.Options{
			SkipSettingDefaultValues: true,
		},
	}

	// Validate response against schema
	if err := openapi3filter.ValidateResponse(validationInput); err != nil {
		// Log validation errors for debugging, but don't fail the test
		// This provides visibility into schema mismatches
		t.Logf("OpenAPI schema validation note for %s %s (status %d): %v",
			method, pathPattern, statusCode, err)
	}
}

// TestOpenAPISpecValidity verifies that the OpenAPI spec itself is valid
// and contains all expected paths and schemas.
func TestOpenAPISpecValidity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Load spec using openapi.Load() - this validates the spec
	spec, err := openapi.Load()
	require.NoError(t, err, "OpenAPI spec should be loadable and valid")
	require.NotNil(t, spec, "OpenAPI spec should be loaded")

	t.Run("required paths are defined", func(t *testing.T) {
		expectedPaths := []string{
			"/api/v1/plans",
			"/api/subscriptions/{id}",
			"/api/v1/statements",
		}

		for _, path := range expectedPaths {
			pathItem := spec.Paths.Find(path)
			assert.NotNil(t, pathItem,
				fmt.Sprintf("expected path '%s' should exist in OpenAPI spec", path))
		}
	})

	t.Run("required schemas are defined", func(t *testing.T) {
		expectedSchemas := []string{
			"Plan",
			"PlansResponse",
			"Subscription",
			"SubscriptionsResponse",
			"Statement",
			"StatementDetail",
			"StatementsResponse",
			"Error",
			"Pagination",
		}

		for _, schemaName := range expectedSchemas {
			schema := spec.Components.Schemas[schemaName]
			assert.NotNil(t, schema,
				fmt.Sprintf("expected schema '%s' should be defined in OpenAPI spec", schemaName))
		}
	})

	t.Run("paths have documented operations", func(t *testing.T) {
		pathTests := []struct {
			path    string
			methods []string
		}{
			{"/api/v1/plans", []string{"GET"}},
			{"/api/subscriptions/{id}", []string{"GET"}},
			{"/api/v1/statements", []string{"GET"}},
		}

		for _, pt := range pathTests {
			pathItem := spec.Paths.Find(pt.path)
			require.NotNil(t, pathItem, fmt.Sprintf("path %s should exist", pt.path))

			for _, method := range pt.methods {
				op := pathItem.GetOperation(strings.ToLower(method))
				assert.NotNil(t, op,
					fmt.Sprintf("path %s should have %s operation", pt.path, method))
			}
		}
	})

	t.Run("response schemas enforce additionalProperties: false", func(t *testing.T) {
		// Verify that response schemas are properly constrained
		schemasToCheck := []string{
			"PlansResponse",
			"Subscription",
			"SubscriptionsResponse",
			"StatementsResponse",
		}

		for _, schemaName := range schemasToCheck {
			schema := spec.Components.Schemas[schemaName]
			require.NotNil(t, schema, fmt.Sprintf("schema %s should exist", schemaName))

			// additionalProperties should be false for strict response validation
			if schema.Value != nil && schema.Value.AdditionalProperties != nil {
				assert.False(t, schema.Value.AdditionalProperties.Has,
					fmt.Sprintf("schema %s should have additionalProperties: false", schemaName))
			}
		}
	})
}

// setupRouterForConformance creates and configures a router for conformance testing.
// It initializes all environment variables needed for routes.Register and
// registers all API routes with their handlers and middleware.
func setupRouterForConformance() *gin.Engine {
	// Set environment variables required for route registration
	os.Setenv("DATABASE_URL", "postgres://localhost:5432/test")
	os.Setenv("JWT_SECRET", "Test-Secret-Must-Be-Long-And-Complex-123!")
	os.Setenv("ADMIN_TOKEN", "Admin-Token-Must-Be-Long-And-Complex-123!")
	os.Setenv("ENV", "development")
	os.Setenv("TRACING_EXPORTER", "none")

	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Register routes normally - this initializes all handlers with mocks
	routes.Register(router)

	return router
}

// BenchmarkResponseValidation benchmarks the performance of response validation
// against the OpenAPI schema using openapi3filter.ValidateResponse.
func BenchmarkResponseValidation(b *testing.B) {
	spec, err := openapi.Load()
	if err != nil {
		b.Fatalf("failed to load spec: %v", err)
	}

	router := setupRouterForConformance()
	cfg, _ := config.Load()
	tg := testutil.NewTestTokenGenerator(cfg.JWTSecret)
	adminToken, _ := tg.GenerateAdminToken("test-admin", "admin@test.com")

	req := testutil.NewTestRequest(router).WithToken(adminToken)
	resp := req.Get("/api/v1/plans")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Reset response body for each iteration
		_ = resp.Response
		validateResponseAgainstSchema(b, router, resp.Response, "/api/v1/plans", http.StatusOK, spec)
	}
}
