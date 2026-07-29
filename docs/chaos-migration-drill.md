# Chaos Migration Drill

A nightly automated drill that validates the migrations runner recovers
correctly when Postgres is killed at a random point during a migration run.

---

## Why this drill exists

Database migrations are rarely tested under failure conditions. If Postgres
crashes or is killed mid-migration, the runner must:

1. Leave the schema in the **pre-migration state** (full rollback via transaction).
2. Re-apply only the **pending migrations** on restart (idempotency).
3. Never corrupt `schema_migrations` with a partial version record.

This drill verifies all three properties on every nightly run.

---

## How it works

```
┌─────────────────────────────────────────────────────────┐
│ 1. Start ephemeral Postgres container                   │
│ 2. Launch `go run ./cmd/migrate up` in background       │
│ 3. Sleep random(KILL_DELAY_MIN, KILL_DELAY_MAX) seconds │
│ 4. docker kill --signal SIGKILL <container>             │
│ 5. Restart the container                                │
│ 6. Re-run `go run ./cmd/migrate up`                     │
│ 7. Assert: migrations succeed, schema_migrations intact │
│ 8. Record result to CSV artifact                        │
└─────────────────────────────────────────────────────────┘
```

The random kill window (default 50ms – 2s) exercises different migration
phases: before DDL, during DDL, after DDL but before the
`schema_migrations` insert, and during the `COMMIT`.

---

## Running locally

```bash
# Default settings (requires Docker)
bash scripts/drills/kill_pg_migration.sh

# Custom kill window
KILL_DELAY_MIN=0.1 KILL_DELAY_MAX=0.5 bash scripts/drills/kill_pg_migration.sh

# With Slack notifications
SLACK_WEBHOOK_URL=https://hooks.slack.com/... bash scripts/drills/kill_pg_migration.sh
```

The script exits 0 on success and 1 on failure.

---

## CI (nightly)

The workflow `.github/workflows/chaos-migration-drill.yml` runs at 02:00 UTC daily.
It can also be triggered manually from the Actions tab with a custom kill window.

It runs on PR when `internal/migrations/**` or `scripts/drills/**` changes.

Results are uploaded as a CSV artifact (`drill-results.csv`) and retained for 30 days.

---

## Output CSV format

```
drill_id,result,recovery_time_ms,kill_delay_s,timestamp,notes
drill-20260728T020000-1234,PASS,312,0.847,2026-07-28T02:00:12Z,"kill_delay=0.847s; applied=14; recovery_ms=312"
```

| Column | Description |
|---|---|
| `drill_id` | Unique run identifier (`drill-<timestamp>-<pid>`) |
| `result` | `PASS`, `FAIL`, or `ERROR` |
| `recovery_time_ms` | Time from container restart to `pg_isready` (ms) |
| `kill_delay_s` | Actual delay used before kill |
| `timestamp` | UTC ISO-8601 |
| `notes` | Free-form notes or error message |

---

## Edge cases verified

| Scenario | Expected behaviour |
|---|---|
| Kill before any DDL executes | Runner re-applies full migration on restart |
| Kill after DDL, before `schema_migrations` INSERT | Transaction rolls back; no orphan tables |
| Kill during `COMMIT` | Postgres WAL ensures atomic commit or full rollback |
| Non-transactional DDL (e.g. `CREATE INDEX CONCURRENTLY`) | Flagged in drill output; requires manual review |
| `schema_migrations` empty after restart | Drill fails with `FAIL` result and Slack alert |

---

## Slack notifications

Set the `CHAOS_DRILL_SLACK_WEBHOOK` repository secret to a valid Slack
incoming webhook URL. The drill posts a success (green) or failure (red)
attachment on every run.

---

## Unit tests

`internal/migrations/chaos_drill_test.go` covers the transactional
properties without Docker:

```bash
go test ./internal/migrations/... -run TestMigration -v
```

Tests:
- `TestMigrationRollbackOnError` — bad SQL leaves no partial `schema_migrations` row
- `TestMigrationIdempotentRecovery` — re-run applies only pending migrations
- `TestMigrationNoPartialRecordOnKill` — cancelled context leaves no corrupt state

---

## Security notes

- The drill container is always named and force-removed on exit (`trap cleanup EXIT`).
- Postgres credentials are hardcoded drill-only values (`drill/drill`) — never production secrets.
- `SLACK_WEBHOOK_URL` is read from the environment and never logged.
- The script validates that `docker` and `awk` are available before touching any containers.
