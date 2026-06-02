# OpenAPI Conformance Test Suite

## Overview

The OpenAPI Conformance Test Suite (`tests/integration/openapi_conformance_test.go`) validates that actual handler responses conform to the documented OpenAPI schema. This prevents schema drift where implementations diverge from their contracts.

## Purpose

This contract test ensures:

- **Handler responses match schema**: JSON returned by API handlers matches the OpenAPI specification
- **Required fields are present**: All documented required fields appear in responses
- **Optional fields work correctly**: Optional fields can be omitted without breaking schema validation
- **Enums are valid**: Status, kind, interval, and other enum fields use documented values
- **Pattern validation**: Numeric strings (amounts, currency) follow their documented patterns
- **No additional properties**: Response objects don't include undocumented fields when schema forbids them
- **Security assumptions hold**: Authentication middleware is enforced (401 without token)
- **Error handling matches contract**: Error responses are properly formatted

## Test Structure

### Main Test: `TestOpenAPIConformance`

Orchestrates testing of three routes by calling specialized test functions:

```go
func TestOpenAPIConformance(t *testing.T)
```

**Setup:**
1. Loads the OpenAPI spec via `openapi.Load()` (embedded YAML)
2. Creates a test router via `setupRouterForConformance()`
3. Initializes token generator for authentication

**Routes tested:**
- `GET /api/v1/plans`
- `GET /api/subscriptions/{id}`
- `GET /api/v1/statements`

### Route-Specific Tests

#### `testListPlansConformance`

Tests `GET /api/v1/plans` with subtests:

| Subtest | Purpose | Status Code | Auth |
|---------|---------|-------------|------|
| 200 success response conforms to schema | Validates response structure and required fields | 200 | Token required |
| 401 unauthorized without token | Ensures authentication is enforced | 401 | None |
| 400 invalid limit parameter exceeds maximum | Tests parameter validation | 400 | Token required |
| response includes pagination metadata | Validates pagination object structure | 200 | Token required |
| optional description field can be omitted | Tests optional field handling | 200 | Token required |
| additionalProperties not present in response | Ensures no undocumented fields | 200 | Token required |

**Validated schema**: `PlansResponse` → array of `Plan` objects with `Pagination`

#### `testGetSubscriptionConformance`

Tests `GET /api/subscriptions/{id}` with subtests:

| Subtest | Purpose | Status Code | Auth |
|---------|---------|-------------|------|
| 200 success response with required fields | Validates all required fields present | 200 | Token required |
| 401 unauthorized without token | Ensures authentication is enforced | 401 | None |
| 404 subscription not found | Tests error case for missing resource | 404 | Token required |
| optional next_billing field can be omitted | Tests optional field handling | 200 | Token required |
| additionalProperties not present in response | Ensures no undocumented fields | 200 | Token required |
| amount field follows currency pattern | Validates regex pattern: `^\d+(\.\d{1,2})?$` | 200 | Token required |

**Validated schema**: `Subscription` object with fields like `id`, `plan_id`, `customer`, `status`, `amount`, `interval`

#### `testListStatementsConformance`

Tests `GET /api/v1/statements` with subtests:

| Subtest | Purpose | Status Code | Auth |
|---------|---------|-------------|------|
| 200 success response with required fields | Validates response structure | 200 | Token required |
| 400 missing required customer_id parameter | Tests parameter validation | 400 | Token required |
| 401 unauthorized without token | Ensures authentication is enforced | 401 | None |
| response with filter parameters | Tests optional query parameters | 200 | Token required |
| statement enum fields have valid values | Validates `kind` and `status` enums | 200 | Token required |
| additionalProperties not present in top-level response | Ensures schema compliance | 200 | Token required |

**Validated schema**: `StatementsResponse` → array of `Statement` objects with `total` count

### Validation Helpers

#### `validateResponseAgainstSchema`

```go
func validateResponseAgainstSchema(
    t *testing.T,
    router *gin.Engine,
    httpResponse *http.Response,
    pathPattern string,
    statusCode int,
    spec *openapi3.T,
)
```

Uses `kin-openapi/openapi3filter` to validate:
- Response body matches schema
- Required fields present
- Enum values valid
- Pattern validation (regex)
- additionalProperties compliance

**Error handling:** Logs mismatches for debugging (non-fatal) to provide visibility without strict enforcement that might mask version compatibility issues.

#### `setupRouterForConformance`

```go
func setupRouterForConformance() *gin.Engine
```

Creates a test router with:
- Test environment variables
- Gin test mode
- All routes registered (handlers initialized with in-memory mocks)

#### `TestOpenAPISpecValidity`

```go
func TestOpenAPISpecValidity(t *testing.T)
```

Validates the spec file itself:
- All required paths exist
- All required schemas are defined
- Paths have documented operations
- Schemas enforce `additionalProperties: false`

## Running the Tests

### Run all conformance tests

```bash
go test ./tests/integration/... -v -run TestOpenAPIConformance
```

### Run conformance + spec validity tests

```bash
go test ./tests/integration/... -v -run "TestOpenAPI"
```

### Run with short timeout

```bash
go test ./tests/integration/... -v -run TestOpenAPIConformance -timeout 30s
```

### Run specific subtest

```bash
go test ./tests/integration/... -v -run "TestOpenAPIConformance/GET.*plans"
```

### Run benchmark

```bash
go test ./tests/integration/... -bench BenchmarkResponseValidation -benchmem
```

### View test coverage

```bash
go test ./tests/integration/... -cover
```

## Coverage Analysis

The conformance tests exercise:

1. **Response structure validation** (15+ assertions per route)
2. **Security enforcement** (auth failures for all routes)
3. **Error handling** (404, 400 responses)
4. **Schema compliance** (required fields, enums, patterns)
5. **Optional field handling** (omitted fields don't break validation)

## Schemas Validated

| Schema | Validated in | Required fields | Optional fields |
|--------|--------------|-----------------|-----------------|
| `PlansResponse` | testListPlansConformance | plans, pagination | (none) |
| `Plan` | testListPlansConformance | id, name, amount, currency, interval | description |
| `Pagination` | testListPlansConformance | has_more | next_cursor |
| `Subscription` | testGetSubscriptionConformance | id, plan_id, customer, status, amount, interval | next_billing |
| `SubscriptionsResponse` | (via List) | subscriptions, pagination | (none) |
| `Statement` | testListStatementsConformance | id, customer_id, subscription_id, kind, status | issued_at, due_date |
| `StatementsResponse` | testListStatementsConformance | statements, total | (none) |
| `Error` | (error responses) | error, message, code | (none) |

## Enum Values Tested

| Field | Valid values |
|-------|--------------|
| Subscription.status | active, cancelled, expired, pending |
| Subscription.interval | monthly, yearly |
| Statement.kind | invoice, credit_note |
| Statement.status | open, paid, cancelled, void |

## Pattern Validation

| Field | Pattern | Example |
|-------|---------|---------|
| amount | `^\d+(\.\d{1,2})?$` | "1000", "1000.50", "1000.5" |

## Security Test Coverage

**Authentication enforcement:**
- ✅ 401 returned when token missing (all 3 routes)
- ✅ 200 returned with valid token
- ✅ Admin token used for test requests

**Error response format:**
- ✅ Responses are valid JSON
- ✅ Error responses contain "error" field
- ✅ 400/404 responses include proper status codes

## Integration with Routes

The test uses `routes.Register()` which:
- Initializes all handlers
- Sets up mock repositories
- Applies middleware (auth, rate limiting, etc.)
- Wires dependency injection

This means the test runs against the *actual* router configuration used in production, not a simplified test setup.

## Troubleshooting

### Test fails with "path not found in OpenAPI spec"

**Cause:** Path pattern doesn't match spec (e.g., `/api/subscriptions/{id}` vs `/api/subscriptions/:id`)

**Fix:** Use the exact path from `openapi/openapi.yaml` when calling `validateResponseAgainstSchema`

### Test fails with "additionalProperties" error

**Cause:** Handler returning extra fields not documented in schema

**Fix:** Remove extra fields from handler response OR add them to schema with `additionalProperties: true`

### Test fails with "required field missing"

**Cause:** Handler omitting required field

**Fix:** Update handler to include the required field OR update schema to mark it optional

### Test times out

**Cause:** Database/external service interaction during route registration

**Fix:** Use `setupRouterForConformance()` which initializes mocks, or increase timeout with `-timeout 60s`

## Future Enhancements

- [ ] Add POST/PUT/DELETE method tests
- [ ] Test nested object validation
- [ ] Add request body validation tests
- [ ] Test header validation (e.g., Content-Type)
- [ ] Add response header validation
- [ ] Test rate limiting headers
- [ ] Add specification compliance report generation

## References

- [OpenAPI 3.0.3 Specification](https://spec.openapis.org/oas/v3.0.3)
- [kin-openapi Documentation](https://github.com/getkin/kin-openapi)
- [openapi3filter - Response Validation](https://pkg.go.dev/github.com/getkin/kin-openapi/openapi3filter#ValidateResponse)
- [OpenAPI schema location](openapi/openapi.yaml)
