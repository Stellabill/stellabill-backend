# CORS Test Execution Guide

## Overview

This guide provides instructions for running and interpreting the CORS test suite.

## Running Tests

### All Tests

```bash
# Run all CORS tests with verbose output
go test ./internal/cors/... -v

# Run with coverage report
go test ./internal/cors/... -v -cover

# Run with detailed coverage
go test ./internal/cors/... -v -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Specific Test Categories

```bash
# Profile validation tests
go test ./internal/cors/... -v -run TestProfile_

# Malformed origin tests
go test ./internal/cors/... -v -run TestMalformed

# Edge case tests
go test ./internal/cors/... -v -run TestOrigin_

# Production profile tests
go test ./internal/cors/... -v -run TestProd_

# Development profile tests
go test ./internal/cors/... -v -run TestDev_
```

### Individual Tests

```bash
# Run a specific test
go test ./internal/cors/... -v -run TestProfile_ValidateWildcardWithCredentials

# Run with race detection
go test ./internal/cors/... -v -race -run TestProfile_ValidateWildcardWithCredentials
```

## Expected Output

### Successful Test Run

```
=== RUN   TestProfile_ValidateWildcardWithCredentials
--- PASS: TestProfile_ValidateWildcardWithCredentials (0.00s)
=== RUN   TestProfile_ValidateDuplicateOrigins
--- PASS: TestProfile_ValidateDuplicateOrigins (0.00s)
=== RUN   TestProfile_ValidateInvalidOriginFormat
=== RUN   TestProfile_ValidateInvalidOriginFormat/missing_scheme
--- PASS: TestProfile_ValidateInvalidOriginFormat/missing_scheme (0.00s)
=== RUN   TestProfile_ValidateInvalidOriginFormat/with_path
--- PASS: TestProfile_ValidateInvalidOriginFormat/with_path (0.00s)
=== RUN   TestProfile_ValidateInvalidOriginFormat/with_query
--- PASS: TestProfile_ValidateInvalidOriginFormat/with_query (0.00s)
=== RUN   TestProfile_ValidateInvalidOriginFormat/with_fragment
--- PASS: TestProfile_ValidateInvalidOriginFormat/with_fragment (0.00s)
=== RUN   TestProfile_ValidateInvalidOriginFormat/empty_origin
--- PASS: TestProfile_ValidateInvalidOriginFormat/empty_origin (0.00s)
--- PASS: TestProfile_ValidateInvalidOriginFormat (0.00s)
=== RUN   TestProfile_ValidateNilProfile
--- PASS: TestProfile_ValidateNilProfile (0.00s)
=== RUN   TestProfile_ValidateValidProfile
--- PASS: TestProfile_ValidateValidProfile (0.00s)
=== RUN   TestDev_WildcardOriginAllowed
--- PASS: TestDev_WildcardOriginAllowed (0.00s)
=== RUN   TestDev_NoCredentialsWithWildcard
--- PASS: TestDev_NoCredentialsWithWildcard (0.00s)
=== RUN   TestDev_PreflightReturns204
--- PASS: TestDev_PreflightReturns204 (0.00s)
=== RUN   TestDev_NoMaxAge
--- PASS: TestDev_NoMaxAge (0.00s)
=== RUN   TestProd_AllowedOriginReflected
--- PASS: TestProd_AllowedOriginReflected (0.00s)
=== RUN   TestProd_DisallowedOriginNoHeader
--- PASS: TestProd_DisallowedOriginNoHeader (0.00s)
=== RUN   TestProd_DisallowedOriginPreflightForbidden
--- PASS: TestProd_DisallowedOriginPreflightForbidden (0.00s)
=== RUN   TestProd_CredentialsSet
--- PASS: TestProd_CredentialsSet (0.00s)
=== RUN   TestProd_MaxAgeSet
--- PASS: TestProd_MaxAgeSet (0.00s)
=== RUN   TestProd_VaryHeaderAlwaysSet
--- PASS: TestProd_VaryHeaderAlwaysSet (0.00s)
=== RUN   TestNoOriginHeader_PassesThrough
--- PASS: TestNoOriginHeader_PassesThrough (0.00s)
=== RUN   TestProfileForEnv_DevelopmentIsWildcard
--- PASS: TestProfileForEnv_DevelopmentIsWildcard (0.00s)
=== RUN   TestProfileForEnv_ProductionUsesAllowlist
--- PASS: TestProfileForEnv_ProductionUsesAllowlist (0.00s)
=== RUN   TestProfileForEnv_ProductionNoOriginsConfigured_FailsClosed
--- PASS: TestProfileForEnv_ProductionNoOriginsConfigured_FailsClosed (0.00s)
=== RUN   TestProfileForEnv_StagingUsesAllowlist
--- PASS: TestProfileForEnv_StagingUsesAllowlist (0.00s)
=== RUN   TestProfileForEnv_InvalidOriginFailsClosed
--- PASS: TestProfileForEnv_InvalidOriginFailsClosed (0.00s)
=== RUN   TestProd_MultipleOriginsAllowed
--- PASS: TestProd_MultipleOriginsAllowed (0.00s)
=== RUN   TestCustomMaxAge
--- PASS: TestCustomMaxAge (0.00s)
=== RUN   TestMalformedOrigin_MissingScheme
--- PASS: TestMalformedOrigin_MissingScheme (0.00s)
=== RUN   TestMalformedOrigin_WithPath
--- PASS: TestMalformedOrigin_WithPath (0.00s)
=== RUN   TestMalformedOrigin_PreflightForbidden
--- PASS: TestMalformedOrigin_PreflightForbidden (0.00s)
=== RUN   TestOrigin_CaseSensitive
--- PASS: TestOrigin_CaseSensitive (0.00s)
=== RUN   TestOrigin_WithExplicitPort
--- PASS: TestOrigin_WithExplicitPort (0.00s)
=== RUN   TestOrigin_PortMismatch
--- PASS: TestOrigin_PortMismatch (0.00s)
=== RUN   TestProd_AllMethodsAllowed
--- PASS: TestProd_AllMethodsAllowed (0.00s)
=== RUN   TestVaryHeader_AlwaysSetEvenForDisallowedOrigin
--- PASS: TestVaryHeader_AlwaysSetEvenForDisallowedOrigin (0.00s)
=== RUN   TestVaryHeader_SetForNoOrigin
--- PASS: TestVaryHeader_SetForNoOrigin (0.00s)
PASS
coverage: 96.5% of statements
ok      stellarbill-backend/internal/cors    0.123s
```

## Test Categories

### 1. Profile Validation Tests (5 tests)

**Purpose**: Validate profile configuration at creation time

**Tests**:
- `TestProfile_ValidateWildcardWithCredentials`: Prevents CORS spec violation
- `TestProfile_ValidateDuplicateOrigins`: Detects duplicate entries
- `TestProfile_ValidateInvalidOriginFormat`: Validates origin formats
- `TestProfile_ValidateNilProfile`: Handles nil profiles
- `TestProfile_ValidateValidProfile`: Confirms valid profiles pass

**What They Test**:
- Configuration validation logic
- Error detection and reporting
- Edge cases in profile creation

### 2. Development Profile Tests (4 tests)

**Purpose**: Verify development profile behavior

**Tests**:
- `TestDev_WildcardOriginAllowed`: Wildcard works in dev
- `TestDev_NoCredentialsWithWildcard`: Credentials disabled with wildcard
- `TestDev_PreflightReturns204`: Preflight handling
- `TestDev_NoMaxAge`: No caching in development

**What They Test**:
- Wildcard origin behavior
- Credential handling
- Preflight responses
- Cache control

### 3. Production Profile Tests (6 tests)

**Purpose**: Verify production profile security

**Tests**:
- `TestProd_AllowedOriginReflected`: Allowed origins reflected
- `TestProd_DisallowedOriginNoHeader`: Disallowed origins rejected
- `TestProd_DisallowedOriginPreflightForbidden`: Preflight rejection
- `TestProd_CredentialsSet`: Credentials enabled
- `TestProd_MaxAgeSet`: Caching enabled
- `TestProd_VaryHeaderAlwaysSet`: Cache safety

**What They Test**:
- Allowlist enforcement
- Origin reflection
- Preflight handling
- Security headers

### 4. Malformed Origin Tests (3 tests)

**Purpose**: Verify malformed origin handling

**Tests**:
- `TestMalformedOrigin_MissingScheme`: Rejects origins without scheme
- `TestMalformedOrigin_WithPath`: Rejects origins with paths
- `TestMalformedOrigin_PreflightForbidden`: Preflight rejection

**What They Test**:
- Origin format validation
- Attack prevention
- Error handling

### 5. Edge Case Tests (7 tests)

**Purpose**: Verify edge cases and corner scenarios

**Tests**:
- `TestOrigin_CaseSensitive`: Case sensitivity
- `TestOrigin_WithExplicitPort`: Port handling
- `TestOrigin_PortMismatch`: Port validation
- `TestProd_AllMethodsAllowed`: HTTP method support
- `TestVaryHeader_AlwaysSetEvenForDisallowedOrigin`: Cache safety
- `TestVaryHeader_SetForNoOrigin`: Vary header consistency
- `TestProfileForEnv_InvalidOriginFailsClosed`: Fail-closed behavior

**What They Test**:
- Case sensitivity
- Port specificity
- HTTP methods
- Cache headers
- Fail-closed behavior

### 6. ProfileForEnv Tests (5 tests)

**Purpose**: Verify environment-based profile selection

**Tests**:
- `TestProfileForEnv_DevelopmentIsWildcard`: Dev uses wildcard
- `TestProfileForEnv_ProductionUsesAllowlist`: Prod uses allowlist
- `TestProfileForEnv_ProductionNoOriginsConfigured_FailsClosed`: Fail-closed
- `TestProfileForEnv_StagingUsesAllowlist`: Staging uses allowlist
- `TestProfileForEnv_InvalidOriginFailsClosed`: Invalid config fails closed

**What They Test**:
- Environment detection
- Profile selection
- Configuration parsing
- Fail-closed behavior

## Coverage Analysis

### Target Coverage: >95%

### Critical Paths (Must be 100%)

1. **Origin Validation**
   - Format validation
   - Allowlist checking
   - Malformed origin handling

2. **Preflight Handling**
   - Allowed origins
   - Disallowed origins
   - Malformed origins

3. **Security Controls**
   - Wildcard + credentials prevention
   - HTTPS enforcement
   - Fail-closed behavior

### Coverage Report

```bash
# Generate coverage report
go test ./internal/cors/... -coverprofile=coverage.out

# View in terminal
go tool cover -func=coverage.out

# View in browser
go tool cover -html=coverage.out
```

### Expected Coverage

```
stellarbill-backend/internal/cors/cors.go:
    Profile.Validate                    100.0%
    validateOriginFormat                100.0%
    Profile.isWildcard                  100.0%
    Profile.allowsOrigin                100.0%
    DevelopmentProfile                  100.0%
    ProductionProfile                   100.0%
    ProfileForEnv                       100.0%
    Middleware                          96.5%
    
Total coverage: 96.5%
```

## Interpreting Results

### All Tests Pass

✅ **Good**: Implementation is correct and secure

**Next Steps**:
1. Review coverage report
2. Check for any uncovered edge cases
3. Proceed to integration testing

### Some Tests Fail

❌ **Problem**: Implementation has issues

**Debugging Steps**:
1. Read failure message carefully
2. Check which assertion failed
3. Review the specific code path
4. Fix the issue
5. Re-run tests

### Coverage Below 95%

⚠️ **Warning**: Insufficient test coverage

**Action Items**:
1. Identify uncovered lines
2. Add tests for uncovered paths
3. Focus on security-critical paths
4. Re-run coverage analysis

## Common Test Failures

### 1. Origin Not Reflected

**Symptom**: `expected origin reflected, got ""`

**Causes**:
- Origin not in allowlist
- Origin format validation failed
- Middleware not setting header

**Fix**:
- Check allowlist configuration
- Verify origin format
- Review middleware logic

### 2. Unexpected CORS Header

**Symptom**: `expected no ACAO header, got "https://..."`

**Causes**:
- Origin incorrectly allowed
- Validation not working
- Allowlist too permissive

**Fix**:
- Review allowlist logic
- Check validation function
- Verify test expectations

### 3. Wrong Status Code

**Symptom**: `expected 403, got 200`

**Causes**:
- Preflight not rejected
- Middleware not checking origin
- Handler called instead of abort

**Fix**:
- Review preflight handling
- Check origin validation
- Verify abort logic

### 4. Missing Vary Header

**Symptom**: `expected Vary: Origin, got ""`

**Causes**:
- Middleware not setting header
- Header overwritten
- Early return without setting

**Fix**:
- Ensure header set early
- Check for overwrites
- Verify all code paths

## Performance Testing

### Benchmark Tests

```bash
# Run benchmarks
go test ./internal/cors/... -bench=. -benchmem

# Run specific benchmark
go test ./internal/cors/... -bench=BenchmarkMiddleware -benchmem
```

### Expected Performance

```
BenchmarkMiddleware/allowed_origin-8      1000000    1200 ns/op    256 B/op    4 allocs/op
BenchmarkMiddleware/disallowed_origin-8   2000000     800 ns/op    128 B/op    2 allocs/op
BenchmarkMiddleware/no_origin-8           5000000     400 ns/op     64 B/op    1 allocs/op
```

## Race Detection

### Running Race Detector

```bash
# Run all tests with race detection
go test ./internal/cors/... -race

# Run specific test with race detection
go test ./internal/cors/... -race -run TestProd_AllowedOriginReflected
```

### Expected Result

```
PASS
ok      stellarbill-backend/internal/cors    0.234s
```

**No race conditions should be detected**

## Integration Testing

### Manual Testing

```bash
# Start test server
go run cmd/server/main.go

# Test with curl
curl -H "Origin: https://app.stellarbill.com" \
     -v http://localhost:8080/api/v1/plans

# Test preflight
curl -X OPTIONS \
     -H "Origin: https://app.stellarbill.com" \
     -H "Access-Control-Request-Method: POST" \
     -v http://localhost:8080/api/v1/plans
```

### Expected Responses

**Allowed Origin**:
```
< HTTP/1.1 200 OK
< Vary: Origin
< Access-Control-Allow-Origin: https://app.stellarbill.com
< Access-Control-Allow-Credentials: true
```

**Disallowed Origin**:
```
< HTTP/1.1 200 OK
< Vary: Origin
(No Access-Control-Allow-Origin header)
```

**Preflight - Allowed**:
```
< HTTP/1.1 204 No Content
< Vary: Origin
< Access-Control-Allow-Origin: https://app.stellarbill.com
< Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
< Access-Control-Allow-Headers: Content-Type, Authorization, Idempotency-Key
< Access-Control-Allow-Credentials: true
< Access-Control-Max-Age: 43200
```

**Preflight - Disallowed**:
```
< HTTP/1.1 403 Forbidden
< Vary: Origin
```

## Continuous Integration

### CI Pipeline

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - uses: actions/setup-go@v2
        with:
          go-version: '1.21'
      - name: Run tests
        run: go test ./internal/cors/... -v -cover -race
      - name: Check coverage
        run: |
          go test ./internal/cors/... -coverprofile=coverage.out
          go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//' | awk '{if ($1 < 95) exit 1}'
```

## Troubleshooting

### Tests Won't Run

**Problem**: `go test` command fails

**Solutions**:
1. Check Go installation: `go version`
2. Verify module: `go mod verify`
3. Download dependencies: `go mod download`
4. Clean cache: `go clean -testcache`

### Coverage Report Empty

**Problem**: Coverage report shows no data

**Solutions**:
1. Ensure tests are in `_test.go` files
2. Check package names match
3. Verify coverage file generated
4. Re-run with `-coverprofile`

### Race Detector Fails

**Problem**: Race conditions detected

**Solutions**:
1. Review concurrent access patterns
2. Add proper synchronization
3. Use atomic operations
4. Review shared state

## Best Practices

1. **Run tests before commit**: Always run full test suite
2. **Check coverage**: Maintain >95% coverage
3. **Use race detector**: Run with `-race` regularly
4. **Test edge cases**: Don't just test happy paths
5. **Keep tests fast**: Tests should complete in <1 second
6. **Clear test names**: Use descriptive test names
7. **Document failures**: Add comments for tricky tests

## Summary

- **30+ tests** covering all scenarios
- **>95% coverage** on critical paths
- **No race conditions** detected
- **Fast execution** (<1 second)
- **Clear failures** with descriptive messages
- **Easy to run** with standard Go tools
