# CORS Security Documentation

## Overview

This package implements a hardened CORS (Cross-Origin Resource Sharing) policy for the Stellarbill API with explicit security controls to prevent common misconfigurations and attacks.

## Security Guarantees

### 1. Explicit Allowlists Only

- **Production/Staging**: Only explicitly configured origins are allowed
- **Development**: Wildcard (`*`) is permitted for local development ergonomics
- **Fail-Closed**: Missing or invalid configuration results in zero allowed origins

### 2. Wildcard Protection

- Wildcard origins (`*`) are **blocked** in production and staging environments
- Wildcard cannot be combined with credentials (CORS spec violation)
- Wildcard cannot be mixed with other origins in the allowlist

### 3. Credential Handling

- Credentials are **enabled** in production/staging (for authenticated requests)
- Credentials are **disabled** with wildcard origins (spec compliance)
- `Access-Control-Allow-Credentials: true` only sent to allowlisted origins

### 4. Origin Validation

All origins undergo strict format validation:
- Must include scheme (e.g., `https://`)
- Must include host
- Must NOT include path, query parameters, or fragments
- Production/staging origins must use HTTPS
- Origins are case-sensitive and port-specific

### 5. No Origin Reflection Attacks

- Origins are validated before reflection
- Malformed origins are rejected without CORS headers
- Only exact matches from the allowlist are reflected
- No substring matching or pattern matching

### 6. Preflight Security

- Disallowed origins receive `403 Forbidden` on preflight
- Malformed origins receive `403 Forbidden` on preflight
- Preflight responses include `Access-Control-Max-Age` for caching
- All preflight responses include `Vary: Origin` header

### 7. Cache Safety

- `Vary: Origin` header is **always** set (even for rejected origins)
- Prevents CDN/proxy from serving wrong CORS policy to different origins
- Preflight cache duration: 12 hours in production, 0 in development

## Configuration

### Environment Variables

```bash
# Required in production/staging
ALLOWED_ORIGINS="https://app.stellarbill.com,https://admin.stellarbill.com"

# Environment (determines profile)
ENV="production"  # or "staging" or "development"
```

### Valid Origin Formats

✅ **Valid**:
```
https://app.stellarbill.com
https://app.stellarbill.com:8443
http://localhost:3000
```

❌ **Invalid**:
```
app.stellarbill.com                    # Missing scheme
https://app.stellarbill.com/path       # Has path
https://app.stellarbill.com?key=val    # Has query
https://app.stellarbill.com#section    # Has fragment
*,https://app.stellarbill.com          # Wildcard mixed with origins
```

### Production Requirements

1. **HTTPS Only**: All origins must use `https://` scheme
2. **No Wildcards**: The `*` origin is rejected
3. **Explicit List**: At least one origin must be configured
4. **Credentials Enabled**: Cookies and auth headers are allowed

### Development Defaults

1. **Wildcard Allowed**: `*` origin is permitted
2. **HTTP Allowed**: Local development can use `http://`
3. **No Credentials**: Credentials disabled with wildcard
4. **No Caching**: Preflight responses not cached

## Attack Prevention

### 1. Origin Reflection Attack

**Attack**: Attacker sends malicious origin, server reflects it back
**Prevention**: Only allowlisted origins are reflected, all others rejected

### 2. Wildcard + Credentials

**Attack**: Wildcard with credentials allows any origin to access authenticated endpoints
**Prevention**: Wildcard and credentials cannot be combined (validation error)

### 3. Subdomain Takeover

**Attack**: Attacker takes over subdomain in wildcard pattern
**Prevention**: No wildcard patterns, only exact origin matches

### 4. Cache Poisoning

**Attack**: CDN caches CORS response for wrong origin
**Prevention**: `Vary: Origin` header always set

### 5. Path Traversal in Origin

**Attack**: Origin with path tricks validation
**Prevention**: Origins with paths are rejected

### 6. Case Manipulation

**Attack**: Attacker uses different case to bypass checks
**Prevention**: Origins are case-sensitive, exact match required

## Testing

### Coverage Requirements

- Minimum 95% test coverage
- All edge cases must be tested
- Security scenarios must have explicit tests

### Key Test Scenarios

1. ✅ Wildcard + credentials validation error
2. ✅ Malformed origins rejected
3. ✅ Disallowed origins receive no CORS headers
4. ✅ Preflight returns 403 for disallowed origins
5. ✅ Case sensitivity enforced
6. ✅ Port specificity enforced
7. ✅ Vary header always present
8. ✅ Invalid config fails closed

## Monitoring

### Metrics to Track

1. **Rejected Origins**: Count of 403 preflight responses
2. **Malformed Origins**: Count of invalid origin formats
3. **Configuration Errors**: Validation failures at startup

### Alerts

1. **High Rejection Rate**: May indicate misconfiguration or attack
2. **Validation Failures**: Configuration issues in production
3. **Wildcard in Production**: Critical security violation

## Compliance

### CORS Specification

- ✅ RFC 6454 (Web Origin Concept)
- ✅ Fetch Standard (CORS protocol)
- ✅ Credentials + wildcard prohibition
- ✅ Preflight caching behavior

### Security Standards

- ✅ OWASP CORS Security Cheat Sheet
- ✅ Fail-closed by default
- ✅ Explicit allowlists only
- ✅ No pattern matching or wildcards in production

## Migration Guide

### From Permissive to Hardened

1. **Audit Current Origins**: Identify all legitimate client origins
2. **Set ALLOWED_ORIGINS**: Configure explicit list
3. **Test Staging**: Verify all clients work with new policy
4. **Deploy Production**: Roll out hardened configuration
5. **Monitor**: Watch for rejected origins

### Example Migration

```bash
# Before (permissive)
# No ALLOWED_ORIGINS set, defaults to wildcard in dev

# After (hardened)
ALLOWED_ORIGINS="https://app.stellarbill.com,https://admin.stellarbill.com"
ENV="production"
```

## Troubleshooting

### Client Receives CORS Error

1. Check origin is in `ALLOWED_ORIGINS`
2. Verify origin format (scheme, no path/query/fragment)
3. Check case and port match exactly
4. Confirm HTTPS in production

### Preflight Returns 403

1. Origin not in allowlist
2. Origin format is invalid
3. Configuration validation failed

### No CORS Headers

1. No `Origin` header in request (same-origin)
2. Origin rejected (not in allowlist)
3. Malformed origin

## References

- [MDN CORS Documentation](https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS)
- [OWASP CORS Security](https://cheatsheetseries.owasp.org/cheatsheets/CORS_Security_Cheat_Sheet.html)
- [Fetch Standard](https://fetch.spec.whatwg.org/#http-cors-protocol)
