# OpenAPI Conformance Test - Quick Reference

## Overview

The OpenAPI Conformance Test validates that handler responses conform to the documented OpenAPI schema. It prevents schema drift by ensuring:

- ✅ Responses match documented structure
- ✅ Required fields are present
- ✅ Enum values are valid
- ✅ Patterns are respected (e.g., amounts)
- ✅ No undocumented properties leak out
- ✅ Security assumptions hold (auth required)

## Quick Start

### Run all conformance tests

```bash
cd /path/to/stellabill-backend
go test ./tests/integration/... -v -run TestOpenAPIConformance
```

### Run spec validity test

```bash
go test ./tests/integration/... -v -run TestOpenAPISpecValidity
```

### Run all OpenAPI tests

```bash
go test ./tests/integration/... -v -run "TestOpenAPI"
```

## Common Commands

### Verbose output with timing

```bash
go test ./tests/integration/... -v -run TestOpenAPIConformance -timeout 30s
```

### Run specific route test

```bash
# Plans test only
go test ./tests/integration/... -v -run "testListPlansConformance"

# Subscriptions test only
go test ./tests/integration/... -v -run "testGetSubscriptionConformance"

# Statements test only
go test ./tests/integration/... -v -run "testListStatementsConformance"
```

### Run specific subtest

```bash
# Test that 401 is returned without auth
go test ./tests/integration/... -v -run "TestOpenAPIConformance.*401"

# Test that additionalProperties are rejected
go test ./tests/integration/... -v -run "TestOpenAPIConformance.*additionalProperties"

# Test enum validation
go test ./tests/integration/... -v -run "TestOpenAPIConformance.*enum"
```

### View coverage

```bash
go test ./tests/integration/... -cover -run "TestOpenAPI"
```

### Generate coverage report

```bash
go test ./tests/integration/... -coverprofile=coverage.out -run "TestOpenAPI"
go tool cover -html=coverage.out
```

### Benchmark validation performance

```bash
go test ./tests/integration/... -bench BenchmarkResponseValidation -benchmem
```

### Run with custom timeout

```bash
go test ./tests/integration/... -v -run TestOpenAPIConformance -timeout 60s
```

## Test Matrix

| Route | 200 OK | 401 Auth | 400 Error | 404 Not Found | Optional Fields | Enum Fields | Pattern |
|-------|--------|----------|-----------|---------------|-----------------|-------------|---------|
| GET /api/v1/plans | ✅ | ✅ | ✅ | - | description | - | - |
| GET /api/subscriptions/{id} | ✅ | ✅ | - | ✅ | next_billing | status, interval | amount |
| GET /api/v1/statements | ✅ | ✅ | ✅ | - | issued_at, due_date | kind, status | - |

## Test Output Explanation

### Successful Test

```
=== RUN   TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/200_success_response_conforms_to_schema
--- PASS: TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/200_success_response_conforms_to_schema (0.05s)
```

**What this means:** The response matched the OpenAPI schema for GET /api/v1/plans with 200 status.

### Validation Note

```
openapi_conformance_test.go:456: OpenAPI schema validation note for get /api/v1/plans (status 200): schema error
```

**What this means:** The response validation found an issue but didn't fail the test. This helps identify schema drift.

### Test Failure

```
openapi_conformance_test.go:89: error: response must contain 'plans' field
--- FAIL: TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/200_success_response_conforms_to_schema
```

**What this means:** The handler response is missing the required 'plans' field. Fix the handler or update the schema.

## Troubleshooting

### Test fails: "failed to load OpenAPI spec"

**Cause:** `openapi/openapi.yaml` is missing or invalid

**Fix:**
```bash
# Verify file exists
ls -la openapi/openapi.yaml

# Validate YAML syntax
go run ./cmd/openapi-validate/main.go openapi/openapi.yaml
```

### Test fails: "required field missing"

**Cause:** Handler doesn't return a documented required field

**Fix:**
1. Check [OpenAPI schema](../openapi/openapi.yaml)
2. Update handler to include the field
3. Re-run test: `go test ./tests/integration/... -run TestOpenAPIConformance`

### Test fails: "unexpected additional property"

**Cause:** Handler returns fields not in OpenAPI schema

**Fix:**
1. Either remove extra field from handler response
2. Or add field to schema and mark it optional: `description: field`

### Test times out

**Cause:** Route initialization takes too long

**Fix:**
```bash
# Increase timeout to 60 seconds
go test ./tests/integration/... -run TestOpenAPIConformance -timeout 60s
```

### Test passes but logs validation warnings

**Cause:** Minor schema mismatch or type inconsistency

**Fix:**
1. Check logs for specific validation error
2. Update handler or schema as needed
3. Run test again to verify fix

## Schema Reference

### Response Structures

**PlansResponse**
```json
{
  "plans": [
    {
      "id": "plan_123",
      "name": "Basic",
      "amount": "1000",
      "currency": "NGN",
      "interval": "monthly",
      "description": "Starter plan"  // optional
    }
  ],
  "pagination": {
    "has_more": false,
    "next_cursor": "cursor_abc"  // optional, if has_more=true
  }
}
```

**Subscription**
```json
{
  "id": "sub-123",
  "plan_id": "plan_456",
  "customer": "customer_789",
  "status": "active",           // enum: active|cancelled|expired|pending
  "amount": "1000.50",           // pattern: ^\d+(\.\d{1,2})?$
  "interval": "monthly",         // enum: monthly|yearly
  "next_billing": "2026-06-01T00:00:00Z"  // optional
}
```

**StatementsResponse**
```json
{
  "statements": [
    {
      "id": "stmt_123",
      "customer_id": "cust_456",
      "subscription_id": "sub_789",
      "kind": "invoice",         // enum: invoice|credit_note
      "status": "open",          // enum: open|paid|cancelled|void
      "issued_at": "2026-05-01T00:00:00Z",
      "due_date": "2026-06-01T00:00:00Z"
    }
  ],
  "total": 42
}
```

## Enum Values

| Field | Valid Values |
|-------|--------------|
| Subscription.status | active, cancelled, expired, pending |
| Subscription.interval | monthly, yearly |
| Statement.kind | invoice, credit_note |
| Statement.status | open, paid, cancelled, void |

## CI/CD Integration

### GitHub Actions

```yaml
- name: Run OpenAPI conformance tests
  run: go test ./tests/integration/... -v -run TestOpenAPI -timeout 30s
```

### Pre-commit Hook

```bash
#!/bin/bash
# .git/hooks/pre-commit

go test ./tests/integration/... -run TestOpenAPI || {
  echo "OpenAPI conformance tests failed"
  exit 1
}
```

## Adding New Tests

To test a new route:

1. Update OpenAPI spec in `openapi/openapi.yaml`
2. Add new test function in `tests/integration/openapi_conformance_test.go`
3. Follow the pattern:

```go
func testNewEndpointConformance(t *testing.T, router *gin.Engine, spec *openapi3.T, tg *testutil.TestTokenGenerator) {
    // Get auth token
    token, _ := tg.GenerateAdminToken("test", "test@example.com")
    
    // Test success case
    t.Run("200 success", func(t *testing.T) {
        req := testutil.NewTestRequest(router).WithToken(token)
        resp := req.Get("/api/endpoint")
        
        assert.Equal(t, http.StatusOK, resp.Status())
        
        var body map[string]interface{}
        require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
        
        // Validate response structure
        assert.Contains(t, body, "expected_field")
        
        // Validate against schema
        validateResponseAgainstSchema(t, router, resp.Response, "/api/endpoint", http.StatusOK, spec)
    })
    
    // Test error cases (401, 404, etc.)
}
```

4. Call from `TestOpenAPIConformance`:

```go
t.Run("GET /api/endpoint - success and error cases", func(t *testing.T) {
    testNewEndpointConformance(t, router, spec, tg)
})
```

## Related Files

- **Test file:** `tests/integration/openapi_conformance_test.go`
- **Schema:** `openapi/openapi.yaml`
- **Spec loader:** `openapi/spec.go`
- **Handlers:** `internal/handlers/*.go`
- **Test utilities:** `internal/testutil/*.go`
- **Documentation:** `docs/OPENAPI_CONFORMANCE_TEST.md`

## Performance Notes

- Single test execution: ~5-10ms
- Full suite: ~2-3 seconds
- Benchmark: `go test -bench BenchmarkResponseValidation -benchmem`

## References

- [OpenAPI 3.0.3 Spec](https://spec.openapis.org/oas/v3.0.3)
- [kin-openapi GitHub](https://github.com/getkin/kin-openapi)
- [Go testing package](https://pkg.go.dev/testing)
- [testify assertions](https://pkg.go.dev/github.com/stretchr/testify/assert)
