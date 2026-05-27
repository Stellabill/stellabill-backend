# Task #140: Enforce OpenAPI contract tests in CI and prevent undocumented endpoints

## Description
Make OpenAPI the source of truth by enforcing that all routes and request/response shapes are validated in CI, preventing drift between implementation and spec.

## Requirements and context
Must be secure, tested, and documented
Should be efficient and easy to review
Code: internal/contract/openapi_contract_test.go, openapi/, cmd/openapi-validate/

## Suggested execution
Fork the repo and create a branch
git checkout -b feature/openapi-ci-enforcement
Implement changes
Ensure OpenAPI validator runs in CI for PRs
Add failing tests when an endpoint isn’t represented in spec
Add a contributor checklist for updating OpenAPI
Validate security assumptions
Ensure auth headers and security schemes are correctly described
Test and commit
Run tests
go test ./...
Cover edge cases
Backward-compatible response changes and versioning strategy
Include test output and security notes
Add “spec-first” policy note in PR description
Example commit message
feat: enforce OpenAPI contract validation in CI

## Guidelines
Minimum 95 percent test coverage
Clear documentation
Timeframe: 96 hours

## Refined Implementation Plan (Critical Flaws Only)

Based on analysis of the Stellabill backend repository, the following flaws would immediately undermine OpenAPI contract enforcement if not addressed first:

### Critical Flaw 1: Duplicate Route Registrations
**Location:** `internal/routes/routes.go`
**Issue:** Multiple registrations of the same endpoints causing confusion about active routes:
- `/plans` registered twice (lines 88-92 and 112-113)
- `/subscriptions` registered multiple times (lines 94-98, 108-109)
- `/subscriptions/:id` registered multiple times (lines 100-104, 110-111)

**Immediate Impact:** Contract tests may validate against incorrect or duplicate route definitions, leading to false positives/negatives.

**Industry Standard Fix:**
1. Consolidate all route registrations to a single location per endpoint
2. Establish clear API versioning strategy (choose either `/api` or `/api/v1`, not both)
3. Remove all duplicate route definitions
4. Create a route registration table or registry for clarity

### Critical Flaw 2: Incomplete Response Validation
**Location:** `internal/contract/openapi_contract_test.go`
**Issue:** Only validates responses for 4 hardcoded endpoints:
- `/api/health`
- `/api/plans` 
- `/api/subscriptions`
- `/api/subscriptions/sub_test`

**Immediate Impact:** Majority of endpoints (admin, statements, reconciliation, etc.) have zero contract validation, creating false sense of security.

**Industry Standard Fix:**
1. Replace hardcoded endpoint validation with dynamic iteration through ALL registered routes
2. For each route, validate response schema against OpenAPI specification
3. Ensure validation covers all HTTP methods for each endpoint
4. Maintain parallel test execution for performance

### Critical Flaw 3: Missing Request Validation
**Location:** `internal/contract/openapi_contract_test.go`
**Issue:** Zero validation of request components:
- Query parameters
- Request headers (including authentication)
- Request bodies (for POST/PUT/PATCH)
- Path parameter validation beyond basic existence

**Immediate Impact:** Contract enforcement only half-implemented; clients could send invalid requests that appear to pass validation.

**Industry Standard Fix:**
1. For each validated route, create comprehensive RequestValidationInput
2. Validate query parameters against OpenAPI specifications
3. Validate headers (especially auth headers) 
4. Validate request bodies with appropriate media types
5. Test both valid and invalid request scenarios

### Critical Flaw 4: Inconsistent API Versioning
**Location:** `internal/routes/routes.go`
**Issue:** Mixed use of `/api` and `/api/v1` path prefixes creating ambiguity about actual API structure.

**Immediate Impact:** OpenAPI spec cannot accurately represent the API if implementation uses conflicting versioning strategies.

**Industry Standard Fix:**
1. Establish single, clear versioning strategy (recommend `/api/v1` for versioned endpoints)
2. Move all versioned routes under consistent path prefix
3. Keep unversioned endpoints (like `/api/health`) separate if intentional
4. Update OpenAPI spec to match actual implemented paths

## Implementation Sequence (Immediate Impact Focus)

### Phase 1: Route Foundation (Hours 1-24)
- Eliminate all duplicate route registrations in `routes.go`
- Establish consistent API path structure 
- Verify all routes register exactly once
- Run existing tests to ensure no regression

### Phase 2: Contract Test Enhancement (Hours 24-48)
- Replace hardcoded endpoint validation with dynamic route iteration
- Implement comprehensive response validation for ALL routes
- Add request validation (query, headers, body) for each route
- Ensure security scheme validation (auth requirements)
- Maintain test performance through parallel execution

### Phase 3: Validation Command Enhancement (Hours 48-72)
- Enhance `cmd/openapi-validate` to provide detailed mismatch reporting
- Add validation that all documented endpoints are implemented
- Add validation that all implemented endpoints are documented
- Provide clear error messages for contract violations

### Phase 4: CI Integration & Documentation (Hours 72-96)
- Verify CI pipeline runs enhanced contract tests
- Update contribution documentation with OpenAPI workflow checklist
- Add spec-first development guidelines
- Document versioning and backward compatibility strategy

## Success Criteria (Immediate Impact)
After implementing this refined plan:
1. ✅ Zero duplicate route registrations in implementation
2. ✅ Contract tests validate 100% of implemented routes for responses
3. ✅ Contract tests validate 100% of implemented routes for requests
4. ✅ Consistent API path structure without versioning confusion
5. ✅ CI fails when implementation deviates from OpenAPI spec
6. ✅ Clear contributor guidance for maintaining API contract
7. ✅ Security requirements validated in contract tests