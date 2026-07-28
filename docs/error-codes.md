# Error Codes

Every error response from the Stellabill API includes a stable `code` field
that clients can use to branch programmatically. Error codes follow the format
`<domain>/<operation-error>` and are defined in the central registry at
`internal/errcode/registry.go`.

**Rule:** never match on the human-readable `message` string — it may change
without notice. Always switch on `code`.

## Response envelope

```json
{
  "code": "subscription/not-found",
  "message": "The requested subscription was not found",
  "trace_id": "550e8400-e29b-41d4-a716-446655440000",
  "details": {}
}
```

| Field      | Type     | Description |
|------------|----------|-------------|
| `code`     | string   | Stable error identifier from the registry. |
| `message`  | string   | Human-readable explanation (may change). |
| `trace_id` | string   | Correlation ID for debugging. |
| `details`  | object   | Optional additional context. |

## Error code registry

### Subscription domain

| Code | HTTP | Description |
|------|------|-------------|
| `subscription/not-found` | 404 | The requested subscription was not found. |
| `subscription/soft-deleted` | 410 | The requested subscription has been deleted. |
| `subscription/forbidden` | 403 | Caller does not have permission to access the subscription. |
| `subscription/invalid-status` | 422 | The provided status value is not a valid subscription status. |
| `subscription/invalid-state-transition` | 409 | The requested status transition is not allowed by the state machine. |
| `subscription/unknown-current-state` | 409 | The subscription has an unrecognized current status. |

### Billing domain

| Code | HTTP | Description |
|------|------|-------------|
| `billing/parse-error` | 500 | Internal error while parsing billing data. |

### Statement domain

| Code | HTTP | Description |
|------|------|-------------|
| `statement/not-found` | 404 | The requested statement was not found. |
| `statement/forbidden` | 403 | Caller does not have permission to access the statement. |

### Plan domain

| Code | HTTP | Description |
|------|------|-------------|
| `plan/not-found` | 404 | The requested plan was not found. |

### Swap domain

| Code | HTTP | Description |
|------|------|-------------|
| `swap/insufficient-liquidity` | 422 | Insufficient liquidity for the requested swap. |
| `swap/invalid-amount` | 400 | The provided swap amount is invalid. |

### Export domain

| Code | HTTP | Description |
|------|------|-------------|
| `export/in-progress` | 409 | An export is already in progress for this tenant. |

### Webhook domain

| Code | HTTP | Description |
|------|------|-------------|
| `webhook/invalid-payload` | 400 | The webhook payload is malformed or missing required fields. |
| `webhook/unknown-event-type` | 422 | The webhook event type is not recognized. |
| `webhook/missing-field` | 400 | A required field is missing from the webhook payload. |

### Authentication / authorization domain

| Code | HTTP | Description |
|------|------|-------------|
| `auth/missing-credentials` | 401 | Authentication credentials are required. |
| `auth/invalid-token` | 401 | The provided authentication credentials are invalid. |
| `auth/forbidden` | 403 | You do not have permission to perform this action. |
| `auth/insufficient-permissions` | 403 | Your role does not have the required permission. |

### Validation domain

| Code | HTTP | Description |
|------|------|-------------|
| `validation/failed` | 400 | The request failed validation. |
| `validation/unknown-field` | 400 | The request contains an unrecognized field. |

### Client errors (generic)

| Code | HTTP | Description |
|------|------|-------------|
| `client/bad-request` | 400 | The request is malformed or contains invalid parameters. |
| `client/not-found` | 404 | The requested resource was not found. |
| `client/conflict` | 409 | The request conflicts with the current state of the resource. |
| `client/rate-limited` | 429 | Too many requests; please retry later. |
| `client/payload-too-large` | 413 | The request payload exceeds the maximum allowed size. |

### Server errors (generic)

| Code | HTTP | Description |
|------|------|-------------|
| `system/internal-error` | 500 | An unexpected internal error occurred. |
| `system/service-unavailable` | 503 | The service is temporarily unavailable. |

## Adding a new error code

1. Open `internal/errcode/registry.go`.
2. Add a `const` declaration for the new code in the appropriate domain section.
3. Add a `mustRegister(...)` call in the `init()` function with the default
   message and HTTP status.
4. Add a test in `internal/errcode/registry_test.go` covering the new code.
5. Reference the code in handlers via `errcode.CodeXxxYyy` (or the `errcode`
   package import if used directly).

The registry panics at init time if a code is registered twice or if an empty
code is provided, so misconfiguration will be caught immediately on startup.

## Mapping service errors

`handlers.MapServiceErrorToResponse` translates domain errors from the service
layer to the appropriate HTTP status and error code:

| Service error | HTTP | Error code |
|---------------|------|------------|
| `ErrNotFound` | 404 | `subscription/not-found` |
| `ErrDeleted` | 410 | `subscription/soft-deleted` |
| `ErrForbidden` | 403 | `subscription/forbidden` |
| `ErrBillingParse` | 500 | `billing/parse-error` |
| `ErrInvalidTransition` | 409 | `subscription/invalid-state-transition` |
| `ErrUnknownCurrentState` | 409 | `subscription/unknown-current-state` |
| `ErrInvalidStatus` | 422 | `subscription/invalid-status` |
| `ErrExportInProgress` | 409 | `export/in-progress` |
| default | 500 | `system/internal-error` |

## Client-side handling example

```typescript
const response = await fetch("/api/v1/subscriptions/123", { headers: auth });
if (!response.ok) {
  const body = await response.json();
  switch (body.code) {
    case "subscription/not-found":
      // Show "subscription not found" UI
      break;
    case "subscription/forbidden":
      // Show "access denied" UI
      break;
    case "subscription/soft-deleted":
      // Show "subscription was cancelled" UI
      break;
    default:
      // Generic error handling
  }
}
```
