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

## Migration immutability

This repository enforces a migration immutability policy for SQL files that have already been merged.
The `scripts/check-migrations.go` script computes SHA256 checksums for every `*.sql` file in `migrations/` and validates them against a registry stored at `migrations/checksums.json`.

- Existing migration files must never be edited once merged.
- New migrations may be added, but `migrations/checksums.json` must be updated with their checksums.
- If `migrations/checksums.json` does not yet exist, the script will generate it and prompt you to commit the new file.

### Adding a new migration

1. Add the new `NNNN_name.up.sql` and `NNNN_name.down.sql` files to `migrations/`.
2. Run:

```bash
go run ./scripts/check-migrations.go
```

3. Commit both the new migration files and the updated `migrations/checksums.json`.

## Safety notes

- Migrations run inside a single transaction per command.
- If a migration fails, the transaction is rolled back and the version is **not** recorded.
- Avoid non-transactional statements (e.g., certain `CREATE INDEX CONCURRENTLY` operations) unless you handle them explicitly.

