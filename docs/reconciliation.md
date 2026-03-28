# Backend ↔ Contract Reconciliation

This document describes the new reconciliation helpers and report model added under `internal/reconciliation`.

What it does
- Defines models for contract snapshots (`Snapshot`) and backend subscriptions (`BackendSubscription`).
- Implements a `Reconciler` that compares the two and returns a `Report` with actionable `FieldMismatch` entries.
- Includes unit tests for matching, mismatch, missing snapshot, and stale snapshot scenarios.

Key comparison points
- status
- amount + currency
- billing interval
- balances (per-key comparison)
- snapshot staleness (contract export older than backend by >24h)

Security notes
- This package is purely local and does not make network calls. When integrating with a live contract adapter:
  - Ensure adapter communication is authenticated and encrypted.
  - Sanitize or redact any PII before persisting or logging reports.
  - Limit access to reconciliation endpoints to privileged roles and audit usage.

Next steps
- Implement an HTTP endpoint in `internal/handlers` that:
  - Accepts a time range / filter and triggers reconciliation by fetching snapshots from the integration adapter.
  - Returns aggregated reports and a summary (counts matched / mismatched).
- Add integration tests that run against a real adapter or a recorded fixture.
