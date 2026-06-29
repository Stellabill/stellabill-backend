# Fee Revenue Materialized View

Pre-aggregated monthly fee revenue for the admin fee-history report, with a
scheduled refresh and freshness metadata.

## Motivation

The admin fee-history report previously scanned raw `statements` rows on every
request. This is `O(rows)` per call and grows unbounded as statement volume
increases. The materialized view `mv_fee_revenue_monthly` pre-aggregates revenue
by tenant and month so the report reads a small, indexed result set instead of
re-scanning the source table.

## Schema

Migration `0012_fee_revenue_mv` creates:

| Object | Purpose |
| --- | --- |
| `mv_fee_revenue_monthly` (materialized view) | Revenue aggregated by `(customer_id, month, currency)`. |
| `uq_mv_fee_revenue_monthly` (unique index) | **Required** for `REFRESH ... CONCURRENTLY`. Grain: `(customer_id, month, currency)`. |
| `idx_mv_fee_revenue_monthly_customer_month` | Serves "by tenant, newest month first" report queries. |
| `mv_fee_revenue_refresh_state` (table) | Single-row freshness metadata holding `last_refreshed_at`. |

### View columns

| Column | Type | Notes |
| --- | --- | --- |
| `customer_id` | text | The billing **tenant** identity. `statements` has no separate `tenant_id`; `customer_id` is the tenant dimension for billing. |
| `month` | timestamptz | Issuance month, truncated to the first day (UTC) via `date_trunc('month', issued_at)`. |
| `currency` | text | Revenue is grouped per currency so amounts are never summed across currencies. |
| `statement_count` | bigint | Number of contributing statements. |
| `total_revenue` | numeric | `SUM(total_amount)`. |

### Source-row filtering

`issued_at` and `total_amount` are stored as `TEXT` (RFC3339 / decimal string)
and are cast explicitly. Only **active, revenue-bearing** statements contribute:

- `deleted_at IS NULL` — excludes soft-deleted statements.
- `archived_at IS NULL` — archived rows have their amount/date nulled out (see
  migration `0010`), so they cannot contribute.
- `issued_at IS NOT NULL AND total_amount IS NOT NULL` — guards the casts
  against the NULLs an archival stub leaves behind.

The view is created `WITH NO DATA`; the first refresh populates it (see below).

## Scheduled refresh

`internal/worker.FeeRevenueRefreshJob` refreshes the view on a fixed interval
(default **hourly**) and records `last_refreshed_at` after each successful
refresh.

```go
job := worker.NewFeeRevenueRefreshJob(db, worker.DefaultFeeRevenueRefreshConfig(), logger)
job.Start()
defer job.Stop()
```

### CONCURRENTLY and the first-refresh fallback

Refreshes use `REFRESH MATERIALIZED VIEW CONCURRENTLY` so in-flight report reads
are **never blocked** — readers keep their snapshot while the refresh builds the
new data in the background.

Postgres rejects `CONCURRENTLY` against a view that has never held data
(created `WITH NO DATA`). The job therefore:

1. Runs the **first** refresh non-concurrently to populate the view.
2. Runs every subsequent refresh **concurrently**.
3. As a defensive fallback, if a concurrent refresh is rejected with a
   "has not been populated" error (e.g. the view was recreated out-of-band),
   retries once non-concurrently and recovers automatically.

### Configuration

| Field | Default | Meaning |
| --- | --- | --- |
| `PollInterval` | `1h` | How often the view is refreshed. |
| `RefreshTimeout` | `5m` | Context timeout for a single refresh. |
| `ShutdownTimeout` | `30s` | Max wait for in-flight work on `Stop()`. |
| `StalenessThreshold` | `2 × PollInterval` | How old `last_refreshed_at` may be before data is flagged stale. |

`Health()` returns an error if the job is stopped or has more than 5 consecutive
failures. `GetStats()` exposes refresh/failure counts and the last error.

## Freshness on the report response

The fee-history report response (`service.FeeHistory`) exposes:

| Field | Type | Meaning |
| --- | --- | --- |
| `last_refreshed_at` | RFC3339 timestamp, omitted when absent | When the view was last refreshed. Omitted when the view has never been refreshed or the report is served from raw data. |
| `stale` | bool, omitted when false | `true` when the data is older than `StalenessThreshold` but is still served (**stale-but-served**). |

The handler annotates the response via `FeeHistory.WithFreshness`, which consults
a `service.FreshnessProvider`. `FeeRevenueRefreshJob.IsStale` satisfies that
interface, so the worker is wired directly to the handler with no DB coupling in
the HTTP layer:

```go
h := handlers.NewFeesHandler(feeService, refreshJob /* FreshnessProvider */)
```

A nil provider leaves the response unannotated (raw-data path). A freshness
**lookup error never fails the report** — the data is still valid, so the
handler serves `200` without the metadata rather than `500`.

## Security & correctness notes

- **No cross-currency summing**: revenue is grouped per currency.
- **No PII expansion**: the view exposes only `customer_id`, month, currency, and
  aggregates — no statement bodies.
- **Reads never blocked**: `CONCURRENTLY` keeps the report available during
  refreshes; a long-running report read keeps its consistent snapshot.
- **Stale-but-served is explicit**: clients can detect delayed data via `stale`
  rather than silently trusting it.
- **Casts are guarded**: NULL `issued_at` / `total_amount` (archival stubs) are
  excluded so a refresh cannot error on a bad cast.

## Tests

Unit tests (no database required):

```bash
go test ./internal/handlers/... ./internal/service/... ./internal/worker/...
```

- `internal/worker/fee_revenue_refresh_job_test.go` — refresh orchestration,
  first-refresh-non-concurrent-then-concurrent, not-populated fallback (and
  fallback-also-fails), freshness recording, **stale-but-served** (`IsStale`),
  never-refreshed, health/stats, start/stop, shutdown timeout, ticker firing.
- `internal/worker/fee_revenue_store_test.go` — `sqlFeeRevenueStore` SQL via
  `go-sqlmock`, asserting the `CONCURRENTLY` keyword and the freshness-state
  read/write (including NULL → never-refreshed and missing-row handling).
- `internal/service/fees_service_test.go` — `WithFreshness` for fresh,
  stale-but-served, never-refreshed, provider-error, and nil cases.
- `internal/handlers/fees_test.go` — report annotation for fresh, stale,
  never-refreshed, and freshness-error-still-serves.

Integration tests (require Docker; build tag `integration`):

```bash
go test -tags=integration ./internal/worker/...
```

- Aggregation by tenant and month against real Postgres.
- **Refresh during a long-running read**: a CONCURRENTLY refresh completes
  without blocking an open read transaction, which keeps its snapshot
  (stale-but-served during refresh).
- Archived and soft-deleted statements are excluded from the aggregate.

> Note: the repository currently does not build as a whole on `main` (unrelated
> corrupt files and an offline module cache), so the suites above were verified
> in isolation. The new code is self-contained: the worker job imports only the
> standard library, and the service/handler changes touch only fee types.
