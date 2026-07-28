# Observability — Prometheus Metrics & Cardinality Policy

This document describes the Prometheus metric instrumentation policy, the guarded registry wrapper that enforces label cardinality limits, and the permitted labels per metric family.

---

## Cardinality Guard

All Prometheus metrics **must** be registered through the `Guard` wrapper in
`internal/metrics/guard.go`. The guard enforces two policies:

### 1. Blocked Label Values

Labels whose values derive from unbounded or user-controlled input are **rejected at registration**. The following label names are blocked:

| Blocked Label    | Reason                                         |
|------------------|------------------------------------------------|
| `raw_path`       | Raw URL paths are unbounded (query params, IDs) |
| `user_id`        | User identifiers can explode cardinality       |
| `customer_id`    | Same as user_id; unbounded per-tenant scale    |
| `subscriber`     | Subscriber identity — unbounded                |
| `email`          | Email addresses are unique per user            |
| `ip` / `ip_address` | IPs vary per request                       |
| `session_id`     | One per session — very high cardinality        |
| `request_id`     | One per HTTP request — extremely high           |
| `trace_id`       | One per trace — extremely high                  |
| `span_id`        | One per span — extremely high                   |
| `bearer_token`   | Rejects accidental token-in-label               |

If a metric definition includes any of these labels, `Register` returns an error and `MustRegister` panics. This ensures **fail-closed at startup**.

### 2. Label Dimension Limit

No metric may declare more than **5 label dimensions** (configurable via `WithMaxLabelsPerMetric`). Metrics with more labels are rejected at registration.

### 3. Runtime Cardinality Limit

Each vec metric (counter, gauge, histogram) may observe at most **500 distinct label-value combinations** (configurable via `WithCardinalityLimit`). When the limit is reached, the guard returns an error for any new combination. This prevents a single misbehaving client or code path from exploding Prometheus storage.

---

## Guard Usage

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "stellarbill-backend/internal/metrics"
)

// Create a guarded registry wrapping the default registerer.
guard := metrics.NewGuard(prometheus.DefaultRegisterer,
    metrics.WithCardinalityLimit(500),
    metrics.WithMaxLabelsPerMetric(5),
)

// Register metrics through the guard.
httpDuration := prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name: "http_request_duration_seconds",
        Help: "HTTP request latency in seconds",
        Buckets: prometheus.DefBuckets,
    },
    []string{"route", "method", "status"},  // allowed
)
guard.MustRegister(httpDuration)

// Convenience constructors are also available.
cv, err := guard.NewCounterVec(
    prometheus.CounterOpts{Name: "my_counter", Help: "..."},
    []string{"label1", "label2"},
)
```

### Tracking Runtime Cardinality

For observation-time enforcement, call `guard.AddLabelCombination(metricName)` before observing a new label-value tuple:

```go
metricName := "http_requests_total"
if err := guard.AddLabelCombination(metricName); err != nil {
    // Cardinality limit reached; skip or log.
    return
}
httpRequestsTotal.WithLabelValues(route, method, status).Inc()
```

---

## Permitted Metrics

| Metric Name                      | Type       | Labels                                | Purpose                             |
|----------------------------------|------------|---------------------------------------|-------------------------------------|
| `http_request_duration_seconds`  | Histogram  | `route`, `method`, `status`           | Request latency per endpoint        |
| `http_requests_total`            | Counter    | `route`, `method`, `status`           | Request count per endpoint          |
| `db_query_duration_seconds`      | Histogram  | `operation`, `table`                  | DB query latency                    |
| `db_queries_total`               | Counter    | `operation`, `table`, `error`         | DB query count with error flag      |
| `db_pool_stats`                  | Gauge      | `stat`                                | Database pool statistics            |
| `cache_hits_total`               | Counter    | `layer`, `op`, `result`               | Cache hit/miss/error counts         |
| `webhook_inbox_lag_seconds`      | Histogram  | (no labels)                           | Webhook processing lag              |

All current metrics in this table are compatible with the guard policy.

---

## Adding a New Metric

1. Choose a name following the `snake_case` convention.
2. Select labels **only** from bounded, low-cardinality sets (e.g. `route`, `method`, `status`, `operation`). **Never** use unbounded values like user IDs, email addresses, or full URLs.
3. Register through the `Guard` (see usage above).
4. Add the metric to the table above.
5. Update this document if the metric has new label names.

---

## Testing

```bash
go test ./internal/metrics/...  -v
```

Tests cover:
- Allowed and blocked labels at registration
- Label dimension limit enforcement
- Duplicate registration (returns `AlreadyRegisteredError`)
- Runtime cardinality tracking (`AddLabelCombination` / `CheckLabelCombination`)
- Unregister cleanup
- Convenience constructors (`NewCounterVec`, `NewGaugeVec`, `NewHistogramVec`)
