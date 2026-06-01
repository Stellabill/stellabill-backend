# Subscription Status Transitions

`POST /api/v1/subscriptions/:id/status` changes a subscription status for the caller's tenant after validating the requested transition against `internal/subscriptions/state_machine.go`.

Security:
- Requires `auth.RequirePermission(auth.PermManageSubscriptions)`.
- Requires `tenantID` on the request context.
- Resolves subscriptions with `FindByIDAndTenant`, so cross-tenant mutations return not found.
- Rejects soft-deleted subscriptions with `410 Gone`.

Request body:

```json
{
  "status": "paused"
}
```

Behavior:
- Valid transitions such as `active -> paused` persist and return `200 OK`.
- No-op transitions such as `active -> active` return `200 OK` with `"changed": false`.
- Unknown target statuses return `422 Unprocessable Entity`.
- Disallowed transitions such as `cancelled -> active` return `409 Conflict`.
- Persisted rows with an unknown current status also return `409 Conflict` so the invalid state is surfaced instead of silently rewritten.

Successful response shape:

```json
{
  "api_version": "v1",
  "data": {
    "id": "sub_123",
    "previous_status": "active",
    "status": "paused",
    "changed": true
  }
}
```
