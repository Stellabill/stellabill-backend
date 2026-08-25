# Vault Database Dynamic Secrets

This document describes how the backend uses Vault's **database secrets
engine** to issue short-lived Postgres credentials and rotate them without a
pod restart. This reduces the blast radius of a leaked credential to the TTL
window configured on the Vault role.

## Overview

Instead of a long-lived `DATABASE_URL` password baked into the deployment, the
backend:

1. **Issues** a fresh set of Postgres credentials from Vault's database
   secrets engine (`POST /v1/{path}/creds/{role}`).
2. **Renews** the lease before it expires (`PUT /v1/sys/leases/renew`).
3. **Rotates** the connection pool when new credentials arrive, draining the
   old pool so in-flight queries finish before the old credentials are
   revoked.

If a renewal fails, the existing lease is used until it expires, then the
process **hard-fails** (the credentials channel is closed and a terminal error
is surfaced) rather than continuing to serve traffic with revoked credentials.

## Components

### `internal/secrets/db_renewer.go`

`secrets.DBRenewer` is a background task that:

- Issues the first credential immediately on `Start`.
- Renews the lease `renewBefore` before expiry.
- Pushes each credential onto a channel consumed by the pool.
- Hard-fails (closes the channel and emits a terminal error) if the lease
  expires without a successful renewal.

```go
renewer := secrets.NewDBRenewer(
    os.Getenv("VAULT_ADDR"),
    os.Getenv("VAULT_TOKEN"),
    "database/creds/app", // role path
    30*time.Second,        // renew 30s before expiry
)
renewer.Start(ctx)
```

### `internal/db/rotating_pool.go`

`db.RotatingPool` wraps a `*pgxpool.Pool` and listens on the credential
channel. When a new credential arrives it:

1. Opens a new pool with the fresh credentials.
2. Atomically swaps the current pool.
3. Drains the old pool in the background.

```go
rp, err := db.NewRotatingPool(ctx, cfg, renewer.Credentials())
```

Callers acquire the current pool via `rp.Pool()`.

## Vault Role Configuration

Enable the database secrets engine and configure a role that issues
short-lived credentials. The role's TTL should be short (e.g. 5 minutes) to
minimize the blast radius of a leaked credential.

```hcl
# Enable the database secrets engine
vault secrets enable database

# Configure the Postgres connection
vault write database/config/postgres \
    plugin_name=postgresql-database-plugin \
    allowed_roles="app" \
    connection_url="postgresql://{{username}}:{{password}}@postgres.internal:5432/app?sslmode=verify-full" \
    username="vault" \
    password="<vault-service-account-password>"

# Create a role that issues short-lived credentials
vault write database/roles/app \
    db_name=postgres \
    creation_statements="CREATE ROLE \"{{name}}\" WITH LOGIN PASSWORD '{{password}}' VALID UNTIL '{{expiration}}'; GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO \"{{name}}\";" \
    default_ttl="5m" \
    max_ttl="15m"
```

The backend issues credentials from `database/creds/app` (the role path passed
to `NewDBRenewer`).

## Environment Variables

| Variable | Description |
|----------|-------------|
| `VAULT_ADDR` | Vault server address (e.g. `https://vault.internal:8200`). |
| `VAULT_TOKEN` | Vault token with permission to read `database/creds/app` and renew leases. |
| `VAULT_DB_ROLE_PATH` | Role path, e.g. `database/creds/app`. |
| `VAULT_DB_RENEW_BEFORE` | How long before expiry to renew (default `30s`). |

## Security Considerations

- **Short TTLs**: Keep `default_ttl` short (minutes, not hours) so a leaked
  credential is only valid for a brief window.
- **Least privilege**: The Vault token should only have `read` on the creds
  path and `update` on `sys/leases/renew`.
- **TLS**: Use `sslmode=verify-full` and a trusted CA for the Postgres
  connection.
- **Hard-fail on expiry**: If the lease cannot be renewed and expires, the
  process fails rather than serving traffic with revoked credentials.

## Testing

```sh
go test ./internal/secrets/... ./internal/db/...
```

The renewer tests use an in-process `httptest.Server` to simulate Vault's
issue and renew endpoints, covering:

- Successful issue + renewal.
- Issue failure (hard-fail).
- Renewal failure → existing lease used until expiry → hard-fail.
- Context cancellation and `Stop()`.
- Malformed responses and missing fields.

The rotating pool tests cover DSN rewriting, nil/empty handling, and (when
`DATABASE_URL` is set) a full rotation against a real database.
