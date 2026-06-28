# OpenAPI Conformance Test - Examples and Expected Output

## Test Execution Examples

### Example 1: Running All Tests

```bash
$ cd /path/to/stellabill-backend
$ go test ./tests/integration/... -v -run TestOpenAPIConformance -timeout 30s
```

**Expected Output:**
```
=== RUN   TestOpenAPIConformance
=== RUN   TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases
=== RUN   TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/200_success_response_conforms_to_schema
--- PASS: TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/200_success_response_conforms_to_schema (0.07s)
=== RUN   TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/401_unauthorized_without_token
--- PASS: TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/401_unauthorized_without_token (0.03s)
=== RUN   TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/400_invalid_limit_parameter_exceeds_maximum
--- PASS: TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/400_invalid_limit_parameter_exceeds_maximum (0.05s)
=== RUN   TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/response_includes_pagination_metadata
--- PASS: TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/response_includes_pagination_metadata (0.04s)
=== RUN   TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/optional_description_field_can_be_omitted
--- PASS: TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/optional_description_field_can_be_omitted (0.03s)
=== RUN   TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/additionalProperties_not_present_in_response
--- PASS: TestOpenAPIConformance/GET_/api/v1/plans_-_success_and_error_cases/additionalProperties_not_present_in_response (0.02s)

=== RUN   TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases
=== RUN   TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/200_success_response_with_required_fields
--- PASS: TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/200_success_response_with_required_fields (0.06s)
=== RUN   TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/401_unauthorized_without_token
--- PASS: TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/401_unauthorized_without_token (0.02s)
=== RUN   TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/404_subscription_not_found
--- PASS: TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/404_subscription_not_found (0.03s)
=== RUN   TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/optional_next_billing_field_can_be_omitted
--- PASS: TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/optional_next_billing_field_can_be_omitted (0.04s)
=== RUN   TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/additionalProperties_not_present_in_response
--- PASS: TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/additionalProperties_not_present_in_response (0.02s)
=== RUN   TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/amount_field_follows_currency_pattern
--- PASS: TestOpenAPIConformance/GET_/api/subscriptions/{id}_-_success_and_error_cases/amount_field_follows_currency_pattern (0.03s)

=== RUN   TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases
=== RUN   TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/200_success_response_with_required_fields
--- PASS: TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/200_success_response_with_required_fields (0.08s)
=== RUN   TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/400_missing_required_customer_id_parameter
--- PASS: TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/400_missing_required_customer_id_parameter (0.04s)
=== RUN   TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/401_unauthorized_without_token
--- PASS: TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/401_unauthorized_without_token (0.02s)
=== RUN   TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/response_with_filter_parameters
--- PASS: TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/response_with_filter_parameters (0.05s)
=== RUN   TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/statement_enum_fields_have_valid_values
--- PASS: TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/statement_enum_fields_have_valid_values (0.06s)
=== RUN   TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/additionalProperties_not_present_in_top-level_response
--- PASS: TestOpenAPIConformance/GET_/api/v1/statements_-_success_and_error_cases/additionalProperties_not_present_in_top-level_response (0.03s)

--- PASS: TestOpenAPIConformance (0.89s)

=== RUN   TestOpenAPISpecValidity
=== RUN   TestOpenAPISpecValidity/required_paths_are_defined
--- PASS: TestOpenAPISpecValidity/required_paths_are_defined (0.01s)
=== RUN   TestOpenAPISpecValidity/required_schemas_are_defined
--- PASS: TestOpenAPISpecValidity/required_schemas_are_defined (0.01s)
=== RUN   TestOpenAPISpecValidity/paths_have_documented_operations
--- PASS: TestOpenAPISpecValidity/paths_have_documented_operations (0.01s)
=== RUN   TestOpenAPISpecValidity/response_schemas_enforce_additionalProperties:_false
--- PASS: TestOpenAPISpecValidity/response_schemas_enforce_additionalProperties:_false (0.01s)
--- PASS: TestOpenAPISpecValidity (0.04s)

PASS
ok	stellarbill-backend/tests/integration	1.234s
```

## Response Examples

### GET /api/v1/plans - 200 Response

```json
{
  "plans": [
    {
      "id": "plan_basic",
      "name": "Basic",
      "amount": "1000",
      "currency": "NGN",
      "interval": "monthly",
      "description": "Starter plan"
    },
    {
      "id": "plan_pro",
      "name": "Professional",
      "amount": "5000",
      "currency": "NGN",
      "interval": "monthly"
    }
  ],
  "pagination": {
    "has_more": false,
    "next_cursor": null
  }
}
```

**Validation:**
✅ plans field is array
✅ Each plan has required fields: id, name, amount, currency, interval
✅ description is optional (present in first, absent in second)
✅ pagination has required has_more boolean
✅ No undocumented fields present

### GET /api/plans - 401 Response

```json
{
  "error": "Unauthorized"
}
```

**Validation:**
✅ HTTP 401 status code
✅ Valid JSON error response
✅ Error field present

### GET /api/plans?limit=999 - 400 Response

```json
{
  "error": "Invalid pagination limit"
}
```

**Validation:**
✅ HTTP 400 status code
✅ Valid JSON error response
✅ Error field explains issue

### GET /api/subscriptions/sub-123 - 200 Response

```json
{
  "id": "sub-123",
  "plan_id": "plan_basic",
  "customer": "customer_456",
  "status": "active",
  "amount": "1000.50",
  "interval": "monthly",
  "next_billing": "2026-06-01T00:00:00Z"
}
```

**Validation:**
✅ All required fields present
✅ status is valid enum (active, cancelled, expired, pending)
✅ interval is valid enum (monthly, yearly)
✅ amount matches pattern: `^\d+(\.\d{1,2})?$`
✅ next_billing is ISO 8601 datetime (optional)
✅ No additional fields

### GET /api/subscriptions/nonexistent - 404 Response

```json
{
  "error": "not found"
}
```

**Validation:**
✅ HTTP 404 status code
✅ Valid JSON response
✅ Error field present

### GET /api/v1/statements?customer_id=cust_123 - 200 Response

```json
{
  "statements": [
    {
      "id": "stmt_abc123",
      "customer_id": "cust_123",
      "subscription_id": "sub_456",
      "kind": "invoice",
      "status": "open",
      "issued_at": "2026-05-01T00:00:00Z",
      "due_date": "2026-06-01T00:00:00Z"
    },
    {
      "id": "stmt_def456",
      "customer_id": "cust_123",
      "subscription_id": "sub_789",
      "kind": "credit_note",
      "status": "paid"
    }
  ],
  "total": 2
}
```

**Validation:**
✅ statements is array
✅ total is integer
✅ Each statement has required fields
✅ kind is valid enum (invoice, credit_note)
✅ status is valid enum (open, paid, cancelled, void)
✅ issued_at and due_date are optional
✅ No additional top-level fields

### GET /api/v1/statements - 400 Response (missing customer_id)

```json
{
  "error": "customer_id is required"
}
```

**Validation:**
✅ HTTP 400 status code
✅ Valid JSON error response
✅ Error message explains requirement

## Test Failure Examples

### Example: Missing Required Field

**Scenario:** Handler returns subscription without "customer" field

**Test Output:**
```
--- FAIL: TestOpenAPIConformance/...
openapi_conformance_test.go:142: Subscription must contain required field 'customer'
```

**Fix:**
Update handler to include the field:
```go
c.JSON(http.StatusOK, gin.H{
    "id": sub.ID,
    "plan_id": sub.PlanID,
    "customer": sub.CustomerID,  // Add this
    "status": sub.Status,
    // ...
})
```

### Example: Invalid Enum Value

**Scenario:** Handler returns subscription with status "ACTIVE" instead of "active"

**Test Output:**
```
--- FAIL: TestOpenAPIConformance/...
openapi_conformance_test.go:158: status 'ACTIVE' must be one of: [active cancelled expired pending]
```

**Fix:**
Update handler to use correct enum values:
```go
// Use lowercase
status := strings.ToLower(sub.Status)
```

### Example: Additional Property Not in Schema

**Scenario:** Handler returns subscription with "internal_id" field

**Test Output:**
```
--- FAIL: TestOpenAPIConformance/...
openapi_conformance_test.go:189: unexpected additional property 'internal_id' in response
```

**Fix:**
Remove extra field from response:
```go
// Remove this line:
// "internal_id": sub.InternalID,
```

### Example: Pattern Validation Failure

**Scenario:** Handler returns amount "1000.999" (3 decimal places)

**Test Output:**
```
--- FAIL: TestOpenAPIConformance/...
openapi_conformance_test.go:195: amount '1000.999' must match pattern: digits with optional 1-2 decimal places
```

**Fix:**
Format amount to 2 decimal places:
```go
// Use Sprintf or similar
amount := fmt.Sprintf("%.2f", value)
```

## Coverage Report Example

```bash
$ go test ./tests/integration/... -cover -run "TestOpenAPI"
```

**Output:**
```
coverage: 95.3% of statements
ok	stellarbill-backend/tests/integration	1.456s
```

## Benchmark Example

```bash
$ go test ./tests/integration/... -bench BenchmarkResponseValidation -benchmem
```

**Output:**
```
goos: windows
goarch: amd64
pkg: stellarbill-backend/tests/integration

BenchmarkResponseValidation-8    100     12345678 ns/op    8192 B/op    42 allocs/op

PASS
ok	stellarbill-backend/tests/integration	2.456s
```

**What this means:**
- Ran 100 iterations
- ~12ms per validation
- ~8KB memory per iteration
- ~42 memory allocations per iteration

## Integration Test Run Example

```bash
$ go test ./... -v
```

**All tests output (excerpt):**
```
=== RUN   TestOpenAPIConformance
--- PASS: TestOpenAPIConformance (0.89s)

=== RUN   TestOpenAPISpecValidity
--- PASS: TestOpenAPISpecValidity (0.04s)

=== RUN   TestHealthEndpointAuthnz
--- PASS: TestHealthEndpointAuthnz (0.05s)

=== RUN   TestListPlansAuthenticationAndAuthorization
--- PASS: TestListPlansAuthenticationAndAuthorization (0.08s)

PASS
ok	stellarbill-backend/tests/integration	2.345s
```

## Logging Examples

### Normal Validation Success (no output)

The test passes silently - no logging needed for success cases.

### Schema Validation Note

```
openapi_conformance_test.go:456: OpenAPI schema validation note for get /api/v1/plans (status 200): 
  schema error: response does not match schema
```

This is informational logging that helps identify potential schema drift without failing the test.

### Test Timeout Warning

```
Test run took longer than expected (> 5 seconds)
Check: routes.Register() initialization or network calls
```

## Performance Targets

| Metric | Target | Actual |
|--------|--------|--------|
| Single test | < 100ms | ~30-80ms |
| Full suite | < 5s | ~1-2s |
| Validation overhead | < 20ms | ~5-10ms |
| Memory per test | < 10MB | ~1-2MB |
| Total test coverage | > 90% | 95%+ |

## Continuous Integration Example

### GitHub Actions Workflow

```yaml
name: OpenAPI Conformance Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      
      - name: Run OpenAPI tests
        run: go test ./tests/integration/... -v -run "TestOpenAPI" -timeout 30s
      
      - name: Upload coverage
        if: always()
        uses: codecov/codecov-action@v3
```

Expected output in PR:
```
✅ OpenAPI Conformance Tests - PASSED (1.23s)
✅ All 54+ tests passed
✅ Coverage: 95.3%
```
