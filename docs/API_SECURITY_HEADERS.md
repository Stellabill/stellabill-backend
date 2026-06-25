# API Security Headers Documentation

This document explains the standard security headers implemented in the Stellabill backend to reduce risk from clickjacking, MIME sniffing, and insecure transport downgrade.

## Implementation Details

The security headers are implemented as a Gin middleware in `internal/middleware/security.go`.

### 1. HTTP Strict Transport Security (HSTS)

HSTS ensures that the browser only communicates with the server over HTTPS.

- **Header**: `Strict-Transport-Security`
- **Rules**:
  - **Production/Staging**: Enabled by default with `max-age=31536000; includeSubDomains`.
  - **Development**: Disabled to allow local testing over HTTP.
- **Configuration**:
  - `SECURITY_HSTS_MAX_AGE`: Configures the `max-age` value (default: `31536000`).

### 2. Content-Security-Policy (CSP): strict nonce-based policy

The application now sends a strict, nonce-based CSP for HTML responses. This prevents inline script injection while still allowing authorized inline scripts when the nonce is supplied by server-generated pages.

- **Header**: `Content-Security-Policy`
- **Policy**: `default-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors <source>; script-src 'self' 'nonce-<generated>'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; form-action 'self'; report-uri <uri>`
- **Default**: `frame-ancestors 'none'` with strict script/style/connect/font form rules.
- **Configuration**:
  - `SECURITY_FRAME_ANCESTORS`: Controls `frame-ancestors` (default: `'none'`).
  - `SECURITY_CSP_REPORT_URI`: Captures browser violations at a configured endpoint (default: `/csp-report`).

This policy is applied by `internal/middleware/security.go` on every request, including HTML responses served by docs and admin pages. The `csp_nonce` context key is also available for pages that need to inject a per-request nonce into inline script tags.

### 3. X-Frame-Options

A legacy header for clickjacking protection, kept for compatibility with older browsers.

- **Header**: `X-Frame-Options`
- **Default**: `DENY`.
- **Configuration**:
  - `SECURITY_FRAME_OPT`: Can be set to `DENY` or `SAMEORIGIN`. Defaults to `DENY` if an insecure value is provided.

### 4. X-Content-Type-Options

Prevents the browser from MIME-sniffing the response away from the declared `Content-Type`.

- **Header**: `X-Content-Type-Options: nosniff`
- **Enforcement**: Always applied.

## Environment-Specific Configuration

| Environment | HSTS     | X-Frame-Options  | CSP frame-ancestors |
| ----------- | -------- | ---------------- | ------------------- |
| Production  | Enabled  | `DENY` (default) | `'none'` (default)  |
| Development | Disabled | `DENY` (default) | `'none'` (default)  |

## Testing

Regression tests are located in `internal/middleware/security_test.go`. These tests assert:

1.  Presence and correctness of headers in production mode.
2.  Omission of HSTS in development mode.
3.  Prevention of insecure `X-Frame-Options` combinations.
4.  No overwriting of headers already set by a proxy layer.
