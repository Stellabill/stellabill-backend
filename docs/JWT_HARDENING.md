# JWT Validation Hardening

## Overview

This document describes the security enhancements made to JWT validation in the authentication layer. These changes prevent token confusion attacks, reject malformed tokens, and enforce strict scope validation.

## Threat Model

### Attacks Mitigated

1. **Algorithm Confusion Attack** (JWT algorithms mismatch)
   - **Attack**: Attacker sends token with different algorithm than expected
   - **Mitigation**: Explicit algorithm validation in keyfunc callback
   - **Test**: `TestAlgorithmValidation`

2. **Token Scope Violation**
   - **Attack**: Reuse of tokens across different audiences/issuers
   - **Mitigation**: Strict issuer/audience validation (exact match required)
   - **Test**: `TestJWTMiddleware` (issuer/audience cases)

3. **Clock Skew Abuse**
   - **Attack**: Attacker exploits excessive clock skew to use expired tokens
   - **Mitigation**: Configurable, bounded clock skew (max 300 seconds)
   - **Test**: `TestClockSkewValidation`

4. **Token Not-Before Bypass**
   - **Attack**: Token used before its validity window
   - **Mitigation**: Explicit NotBefore claim validation
   - **Test**: `TestNotBeforeValidation`

5. **Malformed Token Acceptance**
   - **Attack**: Missing critical claims (exp, aud, iss)
   - **Mitigation**: Required claim validation in `validateClaimsStrict`
   - **Test**: Various test cases in `TestJWTMiddleware`

## Security Features

### 1. Explicit Algorithm Handling

**Code**: [jwt.go](../internal/auth/jwt.go#L63-L73)

```go
// Explicitly validate algorithm to prevent algorithm confusion attacks
if t.Method.Alg() != cfg.Algorithm {
    return nil, fmt.Errorf("unexpected algorithm: expected %s, got %s", cfg.Algorithm, t.Method.Alg())
}
```

**Why**: The `"none"` algorithm or algorithm mismatches can bypass signature validation if not explicitly checked.

**Config Requirement**:

- `Algorithm` field is mandatory (panics if not set)
- Default is `"HS256"` (HMAC-SHA256)

### 2. Strict Issuer/Audience Validation

**Code**: [jwt.go](../internal/auth/jwt.go#L128-L135)

```go
// Validate Issuer (required and must match exactly)
if claims.Issuer != cfg.Issuer {
    return fmt.Errorf("invalid issuer: expected %q, got %q", cfg.Issuer, claims.Issuer)
}

// Validate Audience (required and must contain our audience)
if !stringInSlice(cfg.Audience, claims.Audience) {
    return fmt.Errorf("invalid audience: required %q not found in %v", cfg.Audience, claims.Audience)
}
```

**Why**: Prevents token reuse across different services or environments.

**Guarantees**:

- Issuer must be an exact string match
- Audience must be a list containing the configured value
- Both are mandatory (validation fails if missing)

### 3. Configurable Clock Skew

**Code**: [jwt.go](../internal/auth/jwt.go#L138-L146)

```go
// Validate ExpiresAt with clock skew tolerance
if claims.ExpiresAt == nil {
    return errors.New("token expiration claim missing")
}
expiryTime := claims.ExpiresAt.Time
if now.After(expiryTime.Add(time.Duration(cfg.ClockSkewSec) * time.Second)) {
    return fmt.Errorf("token expired at %v (now: %v, allowed skew: %ds)", expiryTime, now, cfg.ClockSkewSec)
}
```

**Config Parameters**:

- `ClockSkewSec`: Allowed clock drift (seconds)
  - Minimum: 0 (strict, recommended for high-security systems)
  - Default: 0
  - Maximum: 300 (5 minutes, hard limit)
  - Recommended: 30-60 seconds for distributed systems

**Why**: Accounts for clock drift in distributed systems, but bounded to prevent abuse.

### 4. Token Lifetime Validation

**Code**: [jwt.go](../internal/auth/jwt.go#L154-L160)

```go
// Validate IssuedAt to detect token age
if claims.IssuedAt != nil && cfg.MaxTokenAge > 0 {
    issuedTime := claims.IssuedAt.Time
    tokenAge := now.Sub(issuedTime).Seconds()
    if tokenAge > float64(cfg.MaxTokenAge) {
        return fmt.Errorf("token too old: issued %v seconds ago, max age: %ds", int64(tokenAge), cfg.MaxTokenAge)
    }
}
```

**Config Parameters**:

- `MaxTokenAge`: Maximum token age beyond expiry check (seconds, 0 = disabled)
- Use to detect and reject tokens that were issued long ago but not yet expired

### 5. NotBefore Claim Validation

**Code**: [jwt.go](../internal/auth/jwt.go#L148-L153)

```go
// Validate NotBefore with clock skew tolerance
if claims.NotBefore != nil {
    notBeforeTime := claims.NotBefore.Time
    if now.Before(notBeforeTime.Add(-time.Duration(cfg.ClockSkewSec) * time.Second)) {
        return fmt.Errorf("token not valid until %v (now: %v, allowed skew: %ds)", notBeforeTime, now, cfg.ClockSkewSec)
    }
}
```

**Why**: Prevents use of tokens before their validity window.

## Configuration

### Minimal Config (required)

```go
cfg := Config{
    Secret:    []byte("your-32-byte-minimum-secret"),
    Issuer:    "your-service-name",
    Audience:  "your-api-clients",
    Algorithm: "HS256",
}
```

### Recommended Config (production)

```go
cfg := Config{
    Secret:       []byte("your-32-byte-minimum-secret"),
    Issuer:       "stellabill",
    Audience:     "api-clients",
    Algorithm:    "HS256",
    ClockSkewSec: 60,        // Allow 60-second drift
    MaxTokenAge:  86400,     // Reject tokens older than 24 hours
}
```

### Security Validation

The `Config.ValidateConfig()` method enforces:

- Secret must be ≥ 32 bytes
- Issuer must be set
- Audience must be set
- Algorithm must be set
- ClockSkewSec must be 0-300
- MaxTokenAge must be ≥ 0

**Panics on Initialize**: `JWTMiddleware()` will panic if config is invalid—fail fast principle.

## Test Coverage

All security aspects are covered by tests in [jwt_test.go](../internal/auth/jwt_test.go):

| Test Case                 | Threat                       | Requirement          |
| ------------------------- | ---------------------------- | -------------------- |
| `TestConfigValidation`    | Invalid configuration        | Minimum 95% coverage |
| `TestJWTMiddleware`       | Multiple validation failures | All error paths      |
| `TestClockSkewValidation` | Clock skew abuse             | Boundary testing     |
| `TestAlgorithmValidation` | Algorithm confusion          | Accept only HS256    |
| `TestNotBeforeValidation` | Premature token use          | NBF validation       |

### Expected Failures (Rejected Tokens)

These scenarios should return `401 Unauthorized`:

1. ✗ Missing Authorization header
2. ✗ Malformed Authorization header (not "Bearer <token>")
3. ✗ Empty token string
4. ✗ Invalid signature
5. ✗ Token parsing errors (corrupted)
6. ✗ Expired token (beyond clock skew)
7. ✗ Invalid issuer
8. ✗ Invalid audience
9. ✗ Wrong algorithm
10. ✗ NotBefore claim in future
11. ✗ Token too old (MaxTokenAge exceeded)

### Expected Successes (Accepted Tokens)

These scenarios should return `200 OK` (or next handler's response):

1. ✓ Valid token with all required claims
2. ✓ Token within clock skew boundary for expiration
3. ✓ Token with NotBefore in past
4. ✓ Token issued recently (within MaxTokenAge if set)
5. ✓ Correct issuer and audience

## Error Messages

All validation failures return `401 Unauthorized` with JSON:

```json
{
  "error": "specific error message with details"
}
```

Examples:

```json
{"error":"invalid issuer: expected \"stellabill\", got \"malicious\""}
{"error":"token expired at 2024-01-01T10:00:00Z (now: 2024-01-01T10:05:00Z, allowed skew: 30s)"}
{"error":"token too old: issued 86401 seconds ago, max age: 86400s"}
```

## Migration Guide

### Before (Legacy)

```go
cfg := Config{
    Secret:   []byte("secret"),        // No validation of length
    Issuer:   "stellabill",
    Audience: "api-clients",
}
```

### After (Hardened)

```go
cfg := Config{
    Secret:       []byte("min-32-byte-secret-required-now"),
    Issuer:       "stellabill",
    Audience:     "api-clients",
    ClockSkewSec: 60,
    MaxTokenAge:  86400,
    Algorithm:    "HS256",  // Mandatory
}
```

**Breaking Changes**:

- `Algorithm` field is now required
- Secrets < 32 bytes will cause validation failure
- Config is validated on middleware creation (panics if invalid)

## Recommendations

### Security Best Practices

1. **Secret Management**
   - Store in secure secret manager (not in code)
   - Rotate periodically
   - Use ≥ 32 bytes (256 bits) minimum
   - Use cryptographically secure random generation

2. **Algorithm Choice**
   - Use `HS256` (HMAC-SHA256) for symmetric keys
   - Consider `RS256` (RSA) for asymmetric keys
   - Never use "none" algorithm

3. **Token Lifetime**
   - Keep token expiration short (15-60 minutes)
   - Use refresh tokens for longer sessions
   - Set `MaxTokenAge` for additional safety

4. **Clock Skew**
   - Set to 0 in high-security environments
   - Use 30-60 seconds in distributed systems
   - Never exceed 300 seconds

5. **Monitoring**
   - Log all validation failures
   - Alert on repeated failures from same IP/user
   - Track algorithm mismatches (potential attacks)

## Related Documentation

- [JWT RFC 7519](https://tools.ietf.org/html/rfc7519)
- [JSON Web Algorithms RFC 7518](https://tools.ietf.org/html/rfc7518)
- [OWASP JWT Security](https://cheatsheetseries.owasp.org/cheatsheets/JSON_Web_Token_for_Java_Cheat_Sheet.html)
- [Security Analysis](./security-analysis.md)

## Version History

- **v1.0** (2024-04-24): Initial hardened JWT implementation
  - Explicit algorithm validation
  - Strict issuer/audience checks
  - Configurable clock skew with bounds
  - NotBefore validation
  - Comprehensive test suite (95%+ coverage)
