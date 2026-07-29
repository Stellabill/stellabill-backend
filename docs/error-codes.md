# Error Code Registry

## Overview

The Stellabill backend uses a structured error code registry (`internal/errcode`) to assign stable, domain-prefixed identifiers to every error response. Clients branch on error codes rather than message strings, enabling reliable error handling across API versions.

## Format

Error codes follow the pattern `<domain>/<snake-case-slug>`:

```
<domain>/<descriptive-identifier>
```

Examples: `subscription/invalid-state-transition`, `client/not-found`, `system/internal-error`.

## Registry

Error codes are registered in `internal/errcode/registry.go`. Each sentinel error in `internal/service/errors.go` and other service packages is mapped to a stable code via `errcode.Register` at `init()` time.

### Available Domains

| Domain | Description |
|--------|-------------|
| `client` | Client-side errors (bad request, validation, auth, etc.) |
| `subscription` | Subscription lifecycle and billing errors |
| `export` | Tenant data export errors |
| `swap` | Token swap errors |
| `system` | Internal server and service errors |

### Code Reference

#### Client Errors

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `client/bad-request` | 400 | Invalid request parameters or format |
| `client/validation-failed` | 400 | Input validation failed (details in `details`) |
| `client/unauthorized` | 401 | Missing or invalid authentication credentials |
| `client/forbidden` | 403 | Authenticated user lacks permission |
| `client/not-found` | 404 | Requested resource does not exist |
| `client/conflict` | 409 | Request conflicts with current resource state |
| `client/unknown-field` | 400 | Unknown field in request body |

#### Subscription Errors

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `subscription/not-found` | 404 | Subscription not found |
| `subscription/deleted` | 410 | Subscription has been soft-deleted |
| `subscription/forbidden` | 403 | Caller does not own the subscription |
| `subscription/invalid-state-transition` | 409 | Subscription status transition is not allowed |
| `subscription/unknown-state` | 409 | Current subscription status is not a known value |
| `subscription/invalid-status` | 422 | Target status value is not a known subscription status |
| `subscription/billing-parse-error` | 500 | Subscription amount cannot be parsed |

#### Export Errors

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `export/in-progress` | 409 | An export is already in progress for this tenant |

#### Swap Errors

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `swap/insufficient-liquidity` | 422 | Swap cannot be fulfilled due to insufficient liquidity |

#### System Errors

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `system/internal-error` | 500 | Unexpected server error |
| `system/service-unavailable` | 503 | Service temporarily unavailable |

## API Response Envelope

All error responses use the `ErrorEnvelope` structure:

```json
{
  "code": "subscription/invalid-state-transition",
  "message": "Human-readable error description",
  "trace_id": "550e8400-e29b-41d4-a716-446655440000",
  "details": {}
}
```

## Using Error Codes in Code

### Handler helpers

```go
// Generic error response
RespondWithError(c, http.StatusNotFound, errcode.CodeNotFound, "Resource not found")

// Error with additional details
RespondWithErrorDetails(c, http.StatusBadRequest, errcode.CodeValidationFailed,
  "Invalid input", map[string]interface{}{
    "field": "email",
    "reason": "invalid format",
  })

// Specialized helpers
RespondWithAuthError(c, "Missing authentication credentials")
RespondWithValidationError(c, "Field validation failed", details)
RespondWithNotFoundError(c, "subscription")
RespondWithInternalError(c, "Database connection failed")
```

### Service layer

Service errors are automatically mapped via the registry:

```go
// Service returns sentinel errors; handler calls MapServiceErrorToResponse.
statusCode, code, message := MapServiceErrorToResponse(err)
RespondWithError(c, statusCode, code, message)
```

Or directly use `errcode.Lookup` for custom error handling:

```go
code := errcode.Lookup(err)
if code == errcode.CodeSubscriptionInvalidTransition {
    // Handle invalid transition specifically
}
```

### Feature flag errors

Feature flag middleware returns its own structured response:

```json
{
  "error": "feature_unavailable",
  "message": "This feature is currently unavailable",
  "feature_flag": "new_billing_flow"
}
```

## Adding New Error Codes

1. Add the `Code` constant in `internal/errcode/registry.go`
2. Register the matcher in the sending service package's `init()` function using `errcode.Register`
3. Add documentation to this file
4. Add tests verifying the error emits the correct code

**Adding a new error without registering the code will fail CI** — the registry validation tests ensure every error sentinel used in the codebase has a corresponding code entry.

## Testing

Run the error code tests:

```bash
go test ./internal/errcode/... -v
```

Test coverage must be >= 95%.

## Security Notes

- Error codes are stable identifiers; do not include sensitive data in code strings.
- Error messages are redacted via `security.MaskPII` before being sent to clients.
- The `details` field should never contain passwords, tokens, secrets, or PII.
- Trace IDs are included in all error responses for correlation but contain no sensitive data.