# CORS Package

Hardened CORS (Cross-Origin Resource Sharing) implementation with explicit allowlists and comprehensive security controls.

## Quick Start

### Development

```go
// Automatic wildcard for local development
profile := cors.DevelopmentProfile()
router.Use(cors.Middleware(profile))
```

### Production

```bash
# Set environment variable
export ALLOWED_ORIGINS="https://app.stellarbill.com,https://admin.stellarbill.com"
export ENV="production"
```

```go
// Load from environment
profile := cors.ProfileForEnv(cfg.Env, cfg.AllowedOrigins)
router.Use(cors.Middleware(profile))
```

## Configuration

### Environment Variables

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `ENV` | Yes | Environment name | `production`, `staging`, `development` |
| `ALLOWED_ORIGINS` | Production/Staging | Comma-separated origin list | `https://app.example.com,https://admin.example.com` |

### Valid Origin Format

✅ **Correct**:
```
https://app.stellarbill.com
https://app.stellarbill.com:8443
http://localhost:3000
```

❌ **Incorrect**:
```
app.stellarbill.com                    # Missing scheme
https://app.stellarbill.com/           # Trailing slash
https://app.stellarbill.com/path       # Has path
https://app.stellarbill.com?key=val    # Has query
http://app.stellarbill.com             # HTTP in production
```

## Security Features

### Automatic Protection

- ✅ Wildcard blocked in production/staging
- ✅ HTTPS enforced in production/staging
- ✅ Credentials never sent with wildcard
- ✅ Malformed origins rejected
- ✅ Only allowlisted origins receive CORS headers
- ✅ Preflight returns 403 for disallowed origins
- ✅ Cache-safe with Vary: Origin header

### Validation

```go
// Validate a profile before use
profile := cors.ProductionProfile(origins)
if err := profile.Validate(); err != nil {
    log.Fatalf("Invalid CORS profile: %v", err)
}
```

## API Reference

### Types

```go
type Profile struct {
    AllowedOrigins   []string      // Explicit origin allowlist
    AllowedMethods   []string      // HTTP methods
    AllowedHeaders   []string      // Request headers
    AllowCredentials bool          // Enable credentials
    MaxAge           time.Duration // Preflight cache duration
}
```

### Functions

```go
// Create development profile (wildcard)
func DevelopmentProfile() *Profile

// Create production profile (explicit allowlist)
func ProductionProfile(origins []string) *Profile

// Select profile based on environment
func ProfileForEnv(env, rawOrigins string) *Profile

// Create middleware handler
func Middleware(p *Profile) gin.HandlerFunc

// Validate profile configuration
func (p *Profile) Validate() error
```

## Examples

### Basic Usage

```go
package main

import (
    "stellarbill-backend/internal/cors"
    "github.com/gin-gonic/gin"
)

func main() {
    r := gin.Default()
    
    // Development
    profile := cors.DevelopmentProfile()
    r.Use(cors.Middleware(profile))
    
    r.Run(":8080")
}
```

### Production with Validation

```go
origins := []string{
    "https://app.stellarbill.com",
    "https://admin.stellarbill.com",
}

profile := cors.ProductionProfile(origins)

// Validate before use
if err := profile.Validate(); err != nil {
    log.Fatalf("Invalid CORS configuration: %v", err)
}

r.Use(cors.Middleware(profile))
```

### Custom Profile

```go
profile := &cors.Profile{
    AllowedOrigins:   []string{"https://app.example.com"},
    AllowedMethods:   []string{"GET", "POST", "PUT"},
    AllowedHeaders:   []string{"Content-Type", "Authorization"},
    AllowCredentials: true,
    MaxAge:           24 * time.Hour,
}

if err := profile.Validate(); err != nil {
    log.Fatalf("Invalid profile: %v", err)
}

r.Use(cors.Middleware(profile))
```

## Testing

### Run Tests

```bash
# All tests with coverage
go test ./internal/cors/... -v -cover

# Specific test
go test ./internal/cors/... -v -run TestProd_AllowedOriginReflected

# Race detection
go test ./internal/cors/... -race
```

### Coverage Requirements

- Minimum: 95%
- All security scenarios must be tested
- Edge cases must have explicit tests

## Troubleshooting

### CORS Error in Browser

**Symptom**: Browser console shows CORS error

**Solutions**:
1. Check origin is in `ALLOWED_ORIGINS`
2. Verify origin format (no trailing slash, path, query)
3. Confirm exact case and port match
4. Ensure HTTPS in production

### Preflight Returns 403

**Symptom**: OPTIONS request returns 403 Forbidden

**Causes**:
- Origin not in allowlist
- Malformed origin format
- Configuration validation failed

**Solutions**:
1. Add origin to `ALLOWED_ORIGINS`
2. Fix origin format
3. Check server logs for validation errors

### No CORS Headers

**Symptom**: Response has no `Access-Control-Allow-Origin` header

**Causes**:
- No `Origin` header in request (same-origin)
- Origin rejected (not in allowlist)
- Malformed origin

**Solutions**:
1. Verify request includes `Origin` header
2. Check origin is in allowlist
3. Validate origin format

### Configuration Validation Error

**Symptom**: Server fails to start with config error

**Causes**:
- Wildcard in production
- Invalid origin format
- HTTP origin in production
- Duplicate origins

**Solutions**:
1. Use explicit origins in production
2. Fix origin format (include scheme, no path)
3. Use HTTPS in production
4. Remove duplicate entries

## Best Practices

### 1. Explicit Allowlists

```go
// ✅ Good: Explicit list
origins := []string{
    "https://app.stellarbill.com",
    "https://admin.stellarbill.com",
}

// ❌ Bad: Wildcard in production
origins := []string{"*"}
```

### 2. HTTPS in Production

```go
// ✅ Good: HTTPS
"https://app.stellarbill.com"

// ❌ Bad: HTTP in production
"http://app.stellarbill.com"
```

### 3. Validate Configuration

```go
// ✅ Good: Validate before use
profile := cors.ProductionProfile(origins)
if err := profile.Validate(); err != nil {
    return err
}

// ❌ Bad: No validation
profile := cors.ProductionProfile(origins)
r.Use(cors.Middleware(profile))
```

### 4. Environment-Specific Profiles

```go
// ✅ Good: Use ProfileForEnv
profile := cors.ProfileForEnv(cfg.Env, cfg.AllowedOrigins)

// ❌ Bad: Same profile for all environments
profile := cors.DevelopmentProfile()
```

## Security Considerations

### Attack Vectors

1. **Origin Reflection**: Prevented by allowlist validation
2. **Wildcard + Credentials**: Prevented by validation
3. **Cache Poisoning**: Prevented by Vary: Origin header
4. **Subdomain Takeover**: Prevented by exact matching
5. **Path Traversal**: Prevented by format validation

### Monitoring

Track these metrics:
- Rejected preflight requests (403 responses)
- Malformed origin attempts
- Configuration validation failures

### Compliance

- ✅ CORS Specification (Fetch Standard)
- ✅ RFC 6454 (Web Origin Concept)
- ✅ OWASP CORS Security Cheat Sheet

## Additional Resources

- [SECURITY.md](./SECURITY.md) - Comprehensive security documentation
- [MDN CORS Guide](https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS)
- [OWASP CORS Security](https://cheatsheetseries.owasp.org/cheatsheets/CORS_Security_Cheat_Sheet.html)
