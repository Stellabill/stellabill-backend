# Database migrations

This repo uses **file-based SQL migrations** under `migrations/` and a small Go runner (`cmd/migrate`) that tracks applied versions in a `schema_migrations` table.

## Conventions

### File naming

Migrations are paired files:

- `migrations/0001_init.up.sql`
- `migrations/0001_init.down.sql`

Format: `NNNN_name.(up|down).sql`

- `NNNN` is a **positive integer** migration version (sorted ascending).
- `name` is descriptive, using letters/numbers/`_`/`-`.
- Both `up` and `down` files are required.

### Version tracking

Applied migrations are recorded in:

```sql
schema_migrations(version BIGINT PRIMARY KEY, name TEXT, applied_at TIMESTAMPTZ)
```

The runner uses a database transaction and locks `schema_migrations` to avoid concurrent runs.

## Local development

Set `DATABASE_URL` (or pass `-database-url`):

```bash
export DATABASE_URL='postgres://localhost/stellarbill?sslmode=disable'
```

Run migrations:

```bash
go run ./cmd/migrate up
go run ./cmd/migrate status
go run ./cmd/migrate down
```

Dry-run (no DB changes):

```bash
go run ./cmd/migrate --dry-run up
```

## Production runbook (suggested)

1. Back up the database.
2. Run migrations once per deploy (single runner).
3. Monitor logs and fail the deploy if migrations fail.
4. If rollback is required, run `down` **only if** the latest migration is safe to roll back.

## Migration Safety Policies

### Down-Migration Policy
- **Immutability**: Once a migration is merged into `main`, it is immutable. Do not edit existing migrations. If a change is needed, create a new migration.
- **Always Provide Down**: Every `up` migration must have a corresponding `down` migration that completely reverts its changes.
- **Non-Destructive Downs**: Down migrations should ideally not destroy data (e.g., dropping columns with data). If data deletion is unavoidable, ensure backups are verified before rollout.
- **Rollback Window**: Down migrations are intended for immediate rollback during a failed deployment. Do not use down migrations for long-term state reversal.

### Migration Locking Guidance
- **Database-Level Locks**: The migration runner uses `LOCK TABLE schema_migrations IN EXCLUSIVE MODE;` within a transaction. This ensures that even if multiple services or runner instances start concurrently, only one will apply the migrations, preventing race conditions and partial states.
- **Safe Concurrency**: Because of this exclusive lock, it is safe to run the migration tool as an init container or directly on startup across multiple application instances. However, running a single dedicated job is preferred for observability.
- **Timeouts**: The runner applies a context timeout (default 30s) to prevent stalled migrations from holding locks indefinitely.

### Rollback Playbooks
If a deployment fails due to a migration or application error:
1. **Identify the Failure**: Check logs to see if the migration failed to apply, or if the application failed after a successful migration.
2. **Halt Deployment**: Stop further instances from deploying.
3. **Revert Application**: Roll back the application codebase to the previous stable release.
4. **Revert Schema (if necessary)**: If the migration was successfully applied but caused application issues, run `go run ./cmd/migrate down` to revert the schema to match the previous application state.
5. **Verify State**: Confirm the `schema_migrations` table reflects the correct version and the application is healthy.

### Security and Data Integrity
- **Auth Invariants**: Schema changes must never break authentication flows. For instance, do not drop or alter password hashes, MFA secrets, or session tracking tables without a safe transition plan.
- **Data Integrity**: Use foreign keys, `NOT NULL` constraints, and unique indexes to enforce data integrity at the database layer.
- **Transaction Safety**: Avoid non-transactional statements (e.g., `CREATE INDEX CONCURRENTLY`) in standard migrations. If required, they must be executed manually or outside the standard transactional runner to prevent locking issues.


