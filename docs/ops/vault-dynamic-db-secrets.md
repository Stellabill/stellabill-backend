# Vault dynamic database credentials

Configure Vault's database secrets engine to issue a dedicated, short-lived
Postgres role for this service. Do not store generated passwords in deployment
manifests or application configuration.

```hcl
path "database/creds/stellarbill" {
  capabilities = ["read"]
}
path "sys/leases/renew" {
  capabilities = ["update"]
}
```

The `stellarbill` database role must have a bounded default and maximum TTL.
Set the maximum TTL longer than the application's connection-drain window, and
renew before the lease reaches 80% of its lifetime. If renewal fails, keep the
current lease only until expiry, then stop accepting database work and surface a
readiness failure rather than reusing expired credentials.

The application should request `database/creds/stellarbill`, build a Postgres
DSN from the returned username and password, and rotate its pool only after a
new lease has been acquired successfully. Revoke leases during decommissioning.
