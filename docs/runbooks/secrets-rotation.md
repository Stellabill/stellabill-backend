# Secrets Rotation Runbook

## Purpose

This runbook defines the operational rotation procedure for application secrets in `stellabill-backend`.

## Dry-run audit

Fail the build if any secret is past its rotation due date:

```bash
go run ./tools/secrets-audit --dry-run
```

Use a manifest for local/offline validation:

```bash
go run ./tools/secrets-audit --dry-run --manifest ./tools/secrets-audit/testdata/secrets.json
```

## Secrets inventory

| Secret | Owner | Cadence | Verification |
|---|---|---:|---|
| JWT_SECRET | Security | 90d | Mint token, verify acceptance, verify old token rejection |
| JWKS_SECRET | Identity / Platform | 30d | Refresh JWKS and confirm new `kid` resolves |
| DB_PASSWORD | DBA / SRE | 90d | App connectivity, migrations, and smoke query |
| WEBHOOK_SECRET | Integrations | 90d | Send test webhook and verify signature |
| ADMIN_SIGNATURE_SECRET | Backend Platform | 90d | Validate signed admin requests |

## Rotation steps

1. Update the secret in the source of truth.
2. Update rotation metadata with `last_rotated_at`.
3. Deploy the app or reload the secret provider.
4. Run the audit CLI in dry-run mode.
5. Run functional verification for the rotated secret.
6. Revoke the previous secret after the grace window.

## Verification steps by secret

### JWT_SECRET
- Mint a token with the new secret
- Verify the token is accepted by the API
- Verify tokens signed with the old secret fail after cutover

### JWKS_SECRET
- Publish or update JWKS keys
- Confirm the cache refreshes
- Confirm a token with the new `kid` verifies
- Confirm unknown `kid` values are rejected

### DB_PASSWORD
- Update the database credential in the secret store
- Confirm the application can connect
- Run a smoke query
- Confirm migrations still succeed

### WEBHOOK_SECRET
- Update the upstream webhook signing secret
- Send a signed test webhook
- Confirm `internal/middleware/webhook_verification.go` accepts valid signatures
- Confirm stale signatures are rejected

### ADMIN_SIGNATURE_SECRET
- Update the admin request signing secret
- Send a signed admin request
- Confirm request verification succeeds
- Confirm replays and invalid signatures fail

## Failure handling

If the audit fails:
- Identify overdue secrets
- Rotate immediately
- Re-run the CLI
- Record the incident and owner

## Security requirements

- Never place plaintext secrets in the manifest
- Metadata must contain only operational data
- The CLI is read-only
- Rotation verification should be repeatable and auditable

## Nightly validation

The audit should run nightly in CI to catch overdue secrets before they expire unexpectedly:

```bash
go run ./tools/secrets-audit --dry-run
```

## Notes

- If a secret provider does not expose metadata, use the manifest as the source of truth.
- The audit fails if metadata is missing or if a secret is past due.