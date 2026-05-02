# Backend ↔ Contract Reconciliation

This document describes the reconciliation helpers, report model, and RBAC-scoped endpoints under `internal/reconciliation` and `internal/handlers`.

## What it does
- Defines models for contract snapshots (`Snapshot`) and backend subscriptions (`BackendSubscription`), both carrying a `TenantID` for isolation.
- Implements a `Reconciler` that compares the two and returns a `Report` with actionable `FieldMismatch` entries.
- Includes unit tests for matching, mismatch, missing snapshot, and stale snapshot scenarios.

## Key comparison points
- status
- amount + currency
- billing interval
- balances (per-key comparison)
- snapshot staleness (contract export older than backend by >24h)

## RBAC & tenant scoping

### Permissions
| Permission | Admin | Merchant | Customer |
|---|---|---|---|
| `manage:reconciliation` | ✓ | — | — |
| `read:reconciliation` | ✓ | ✓ | — |

### Endpoint access
- `POST /api/admin/reconcile` — requires `manage:reconciliation`. Admins can reconcile any subscription. Merchants are restricted to their own tenant's subscriptions; any cross-tenant submission is rejected with 403.
- `GET /api/admin/reports` — requires `read:reconciliation`. Admins see all reports. Merchants see only their tenant's reports.

### Tenant isolation
- All models (`Snapshot`, `BackendSubscription`, `Report`) carry a `TenantID` field.
- Non-admin callers have `TenantID` stamped onto every submitted subscription automatically; the adapter snapshot list is filtered to exclude other tenants' data.
- The `Store.ListReportsByTenant(tenantID)` method enforces server-side filtering.

### Cursor security
Pagination cursors are HMAC-signed and embed the `TenantID`. On decode, the handler validates:
1. The HMAC signature is intact (prevents tampering).
2. The embedded tenant matches the caller's tenant (prevents IDOR via cursor replay).

Set the `CURSOR_HMAC_SECRET` environment variable in production. A default key is used in development.

## Security notes
- This package is purely local and does not make network calls. When integrating with a live contract adapter:
  - Ensure adapter communication is authenticated and encrypted.
  - Sanitize or redact any PII before persisting or logging reports.
  - Limit access to reconciliation endpoints to privileged roles and audit usage.
- Predictable IDs are not directly exposed; reports are listed via tenant-scoped queries, not by guessable identifiers.
