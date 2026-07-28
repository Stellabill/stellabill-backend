# Business KPI Dashboards

The `/metrics` endpoint exposes three business-health gauges alongside the
existing system metrics (`http_request_duration_seconds`, `db_pool_stats`, etc.)
so operators can graph business KPIs on the same dashboard as infrastructure
metrics.

## Available Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `mrr_cents_total` | Gauge | — | Monthly Recurring Revenue in cents across all active subscriptions. Divide by 100 to get dollars. |
| `active_subscribers_total` | GaugeVec | `plan_id`, `plan_name` | Number of active subscriptions, broken down by plan. |
| `churn_rate_24h` | Gauge | — | Fraction of subscriptions cancelled in the last 24 hours relative to the active + recently-cancelled base. Range: `0.0` – `1.0`. Multiply by 100 for percentage. |

## Refresh Cadence

All three metrics are computed on a **fixed 1-hour schedule** by
`internal/worker.KpiRefreshJob` to avoid expensive aggregation queries at
scrape time. On server startup the job runs immediately so the dashboard shows
data within seconds; subsequent refreshes fire every hour.

## Example PromQL Queries

### Monthly Recurring Revenue (USD)

```promql
mrr_cents_total / 100
```

### Active Subscribers by Plan (stacked area)

```promql
sum by (plan_id, plan_name) (active_subscribers_total)
```

### Churn Rate Percentage

```promql
churn_rate_24h * 100
```

### Churn Rate Alert (warning at 5 %, critical at 10 %)

```promql
churn_rate_24h * 100 > 5
```

### New Subscribers This Hour (delta)

```promql
delta(mrr_cents_total[1h]) / 100
```

## Security

The `/metrics` endpoint does not require authentication because:

1. **Network-level access control** is expected — Kubernetes `NetworkPolicy`,
   a sidecar auth-proxy, or cloud load-balancer ACLs restrict who can reach
   `/metrics`.
2. **No PII** is exposed. MRR, subscriber counts, and churn rate are
   aggregates; no individual customer or subscription identifiers appear in
   the metric labels.

If your deployment requires HTTP-level auth for `/metrics`, add a
metrics-specific middleware in `routes.Register`:

```go
r.GET("/metrics", metricsAuthMiddleware, gin.WrapH(promhttp.Handler()))
```

## Configuration

| Field | Default | Description |
| --- | --- | --- |
| `PollInterval` | `1h` | How often KPI metrics are recomputed. |
| `QueryTimeout` | `30s` | Context timeout for the three aggregation queries. |
| `ShutdownTimeout` | `30s` | Max time to wait for in-flight computation on graceful shutdown. |

## Visibility

The job exposes its own health and statistics via `Health()` and `GetStats()`.
Refer to the [elevated-errors runbook](./elevated-errors-runbook.md) for
alerting on worker failures: consistently failing KPI refreshes (5+
consecutive errors) will cause `Health()` to return an error and should
trigger a PagerDuty alert.

## Suggested Grafana Panels

### Panel 1: MRR (USD)

- Query: `mrr_cents_total / 100`
- Type: Stat (current value) + Time series
- Unit: USD
- Threshold: $0 (red if zero)

### Panel 2: Active Subscribers

- Query: `sum by (plan_name) (active_subscribers_total)`
- Type: Stacked bar or stacked area
- Legend: `{{plan_name}}`

### Panel 3: Churn Rate

- Query: `churn_rate_24h * 100`
- Type: Stat (current %) + Time series
- Unit: Percent (0-100)
- Alert: Warning at 5 %, Critical at 10 %

## Maintainability

- **Gauges are defined in `internal/metrics/metrics.go`** via `promauto` so
  they auto-register with the default Prometheus registry.
- **The store interface** (`kpiStore` in `internal/worker/kpi_refresh_job.go`)
  makes the SQL layer testable with `go-sqlmock` without a real database.
- **The job lifecycle** (Start/Stop/Health/GetStats) follows the same pattern
  as `FeeRevenueRefreshJob` and `StatementArchiveJob`, keeping the codebase
  consistent.