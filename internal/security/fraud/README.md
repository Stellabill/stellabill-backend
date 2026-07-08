# Fraud Signal Collector

`internal/security/fraud` is an aggregate, **tenant-scoped** detector for abusive
request patterns. It complements the per-request guards in
[`internal/middleware`](../../middleware) (auth, rate limiting) by correlating
events across a sliding time window and emitting structured audit events when a
tenant's behavior crosses a configured threshold.

## What it detects

| Signal                     | Meaning                                                        | Attack it surfaces        |
| -------------------------- | ------------------------------------------------------------- | ------------------------- |
| `auth_fail_rate`           | Authentication failures per tenant per window                 | Credential stuffing       |
| `subscription_id_misses`   | Lookups for subscription IDs not owned by the tenant          | ID / resource enumeration |
| `plan_churn_rate`          | Plan changes per tenant per window                            | Rapid plan churn / abuse  |

When a signal trips, a canonical `fraud.signal.detected` audit event is written
through the project's [`internal/audit`](../../audit) logger (hash-chained,
append-only).

## Design

- **Sliding window** (`window.go`): each signal is a ring of sub-window buckets.
  Counts expire bucket-by-bucket as time advances, giving accurate roll-over
  without retaining an unbounded event list. Finer `Buckets` → smoother decay.
- **Clock injection**: all time comes from a `Clock` interface, normalized to
  UTC. This makes window roll-over and clock-skew behavior deterministically
  testable, and keeps math independent of the process timezone.
- **Concurrency**: the collector shards state per tenant behind a `sync.RWMutex`,
  with a per-tenant mutex guarding each tenant's signal windows. Safe for
  concurrent use from request handlers.
- **Cooldown**: once a tenant trips a signal, repeat emissions are suppressed for
  `Cooldown` to avoid event storms while the tenant stays hot.
- **Memory bound**: call `EvictIdle()` periodically (e.g. from a ticker) to drop
  tenants whose windows are empty and whose last activity predates `IdleTTL`.

## Privacy

The collector **never accepts or stores raw PII or credentials** — only an
opaque tenant scope key and event counts cross its boundary. Tenant identifiers
are HMAC-hashed (`HashSecret`) before appearing in any emitted event, so events
correlate the same tenant without revealing its identity. There is no code path
that logs tokens, passwords, or raw IDs.

## Usage

```go
import (
    "stellarbill-backend/internal/audit"
    "stellarbill-backend/internal/security/fraud"
)

logger := audit.NewLogger(secret, sink)
collector := fraud.NewCollector(fraud.DefaultConfig(), fraud.Adapt(logger))

// Periodically reclaim memory for idle tenants.
go func() {
    t := time.NewTicker(time.Minute)
    defer t.Stop()
    for range t.C {
        collector.EvictIdle()
    }
}()
```

Wire the observers at the points where the corresponding events occur:

```go
// internal/middleware/auth.go — on a failed token validation:
collector.ObserveAuthFailure(tenantID)

// when a subscription lookup resolves to a row not owned by the tenant:
collector.ObserveSubscriptionIDMiss(tenantID)

// in the plan-change handler, after a successful plan mutation:
collector.ObservePlanChange(tenantID)
```

Each `Observe*` call returns `true` only when it emitted an event on that call
(threshold crossed and not within cooldown). Passing `nil` as the emitter to
`NewCollector` runs the detector in **shadow mode**: signals are counted but
nothing is published — useful for tuning thresholds before enforcement.

## Configuration

`DefaultConfig()` provides production-leaning windows and thresholds. Override
per signal via `Config.Signals`:

```go
cfg := fraud.Config{
    Signals: map[fraud.Signal]fraud.SignalConfig{
        fraud.SignalAuthFailRate: {
            Window:    time.Minute,
            Buckets:   12,
            Threshold: 20,
            Cooldown:  time.Minute,
        },
    },
    HashSecret: os.Getenv("FRAUD_HASH_SECRET"),
}
```

A `Threshold <= 0` keeps a signal counted but never emits (monitoring only).

## Tests

```sh
go test ./internal/security/... -cover
```

Coverage is ~99% of statements. Edge cases covered include window roll-over
(full and partial), ring-buffer reuse, hot-tenant bursts, cooldown suppression,
clock-skew (backward/forward jumps) and non-UTC clock normalization, tenant
isolation, idle eviction, emitter-failure tolerance, and end-to-end assertions
that no raw tenant identifier leaks into persisted audit output.
