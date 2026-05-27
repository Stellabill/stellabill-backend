# DB Pool Tuning — Ops Runbook

## Overview

The production database connection pool is managed by `internal/db/pool.go`
using `pgxpool` (jackc/pgx/v5).  All tuning knobs are driven by environment
variables so they can be changed without recompiling.

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DB_POOL_MAX_CONNS` | `25` | Hard ceiling on open connections. Leave headroom for other clients (migrations, admin tools). Rule of thumb: `(Postgres max_connections × 0.8) / app_instances`. |
| `DB_POOL_MIN_CONNS` | `2` | Connections kept warm at all times. Prevents cold-start latency on the first request after a quiet period. |
| `DB_POOL_MAX_CONN_LIFETIME` | `3600` (1 h) | Recycle connections after this many seconds. Spreads load across replicas and avoids stale TCP sessions after a Postgres restart. |
| `DB_POOL_MAX_CONN_IDLE_TIME` | `600` (10 min) | Evict idle connections after this many seconds. Prevents silent firewall drops on long-idle TCP sessions. Must be less than `DB_POOL_MAX_CONN_LIFETIME`. |
| `DB_POOL_CONNECT_TIMEOUT` | `5` | Per-dial timeout in seconds. Surfaces misconfigurations at startup rather than hanging indefinitely. |
| `DB_POOL_HEALTH_CHECK_PERIOD` | `30` | How often pgxpool proactively checks idle connections (seconds). |
| `DB_POOL_METRICS_INTERVAL` | `15` | How often pool statistics are scraped into Prometheus gauges (seconds). |

Validation bounds: `DB_POOL_MAX_CONNS` 1–500, all timeouts 1–300 s.
Invalid values produce a **warning** (not a hard error) and fall back to the
default so the server can still start.

---

## Prometheus Metrics

| Metric | Type | Alert threshold |
|---|---|---|
| `db_pool_acquired_conns` | Gauge | — |
| `db_pool_idle_conns` | Gauge | — |
| `db_pool_total_conns` | Gauge | — |
| `db_pool_max_conns` | Gauge | — |
| `db_pool_constructing_conns` | Gauge | Sustained > 0 under load → pool ceiling too low |
| `db_pool_acquire_count_total` | Counter | — |
| `db_pool_canceled_acquire_total` | Counter | Any non-zero in production → pool exhausted |
| `db_pool_empty_acquire_total` | Counter | Rising rate → pool ceiling too low |
| `db_pool_acquire_duration_seconds` | Histogram | p99 > 500 ms → saturation |

### Recommended Alerts (Prometheus / Alertmanager)

```yaml
- alert: DBPoolSaturated
  expr: db_pool_acquired_conns / db_pool_max_conns > 0.85
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "DB pool is >85% saturated"
    description: "Increase DB_POOL_MAX_CONNS or scale horizontally."

- alert: DBPoolAcquireLatencyHigh
  expr: histogram_quantile(0.99, rate(db_pool_acquire_duration_seconds_bucket[5m])) > 0.5
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "DB pool acquire p99 > 500 ms"

- alert: DBPoolCanceledAcquires
  expr: rate(db_pool_canceled_acquire_total[5m]) > 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "DB pool acquire timeouts detected"
    description: "Requests are failing to get a DB connection. Check pool size and Postgres health."
```

---

## Sizing Guide

```
DB_POOL_MAX_CONNS = floor((postgres_max_connections * 0.8) / app_instances)
```

Example: Postgres `max_connections=100`, 4 app instances → `floor(80/4) = 20`.

Start conservative (25) and increase based on `db_pool_acquired_conns /
db_pool_max_conns` saturation ratio.

---

## Partial Outage Behaviour

- `DB_POOL_CONNECT_TIMEOUT` (default 5 s) ensures a dead Postgres host is
  detected within 5 seconds per dial attempt rather than blocking indefinitely.
- `DB_POOL_MAX_CONN_IDLE_TIME` (default 600 s) evicts connections that were
  silently dropped by a firewall during a partial outage, so the pool
  self-heals when Postgres comes back.
- `DB_POOL_MAX_CONN_LIFETIME` jitter (10 % of lifetime) prevents a
  thundering-herd of simultaneous reconnects after a Postgres restart.

### Idempotency note

All write operations in this service use idempotency keys (see
`internal/idempotency`).  A context-cancelled DB acquire (due to
`DB_POOL_CONNECT_TIMEOUT`) will return an error to the caller **before** any
SQL is executed, so there is no risk of a partial write.  Retrying with the
same idempotency key is safe.

---

## Leak Detection

A connection "leak" manifests as `db_pool_acquired_conns` growing without
bound while `db_pool_idle_conns` stays at zero.

Checklist:
1. Every `pool.Acquire()` call must be paired with `conn.Release()` (use
   `defer conn.Release()`).
2. Every `pool.Begin()` transaction must be committed or rolled back.
3. Set a query-level context deadline so a slow query releases its connection
   when the deadline fires.

---

## Runbook: Pool Exhaustion

1. Check `db_pool_canceled_acquire_total` — if rising, the pool is exhausted.
2. Check `db_pool_constructing_conns` — if sustained > 0, Postgres is slow to
   accept new connections (network issue or `max_connections` reached).
3. Increase `DB_POOL_MAX_CONNS` (redeploy or hot-reload via env).
4. If Postgres `max_connections` is the bottleneck, add a PgBouncer layer.
5. Check for leaks: `db_pool_acquired_conns` should return to baseline after
   traffic drops.
