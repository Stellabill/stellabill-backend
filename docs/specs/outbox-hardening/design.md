# Design Document: Outbox Hardening

## Overview

This document describes the technical design for hardening the Stellabill outbox pattern implementation (`internal/outbox/`) to provide exactly-once publish semantics, robust retry/backoff with jitter, failure injection tests, PII-safe payloads, dead-letter queue observability, and operational runbooks.

The existing implementation has the right skeleton: a `postgresRepository`, a polling `dispatcher`, a `Service`, and a `Manager`. The gaps are:

1. `PublishEventWithTx` does not actually use the caller's `*sql.Tx` — it calls the regular repository which opens its own connection.
2. No deduplicate key exists, so downstream consumers cannot detect duplicate deliveries.
3. Events stuck in `processing` after a crash are never recovered.
4. Retry backoff has no jitter, enabling retry storms.
5. Event payloads and log lines may contain PII.
6. No chaos/failure injection test infrastructure exists.
7. No dead-letter query or requeue API exists.
8. No operational runbook exists.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Business Handler                                               │
│  tx := db.BeginTx(...)                                          │
│  UPDATE business_table ...                                      │
│  service.PublishEventWithTx(tx, ...)  ──► StoreWithTx(tx, ...) │
│  tx.Commit()                                                    │
└─────────────────────────────────────────────────────────────────┘
                          │ atomic commit
                          ▼
              ┌───────────────────────┐
              │   outbox_events table │
              │  (dedupe_key UNIQUE)  │
              └───────────┬───────────┘
                          │ poll every PollInterval
                          ▼
              ┌───────────────────────┐
              │      Dispatcher       │
              │  1. RecoverStuck      │
              │  2. GetPending        │
              │  3. MarkProcessing    │
              │  4. Publish (+ panic  │
              │     recovery)         │
              │  5. UpdateStatus /    │
              │     IncrementRetry    │
              └───────────┬───────────┘
                          │
                          ▼
              ┌───────────────────────┐
              │      Publisher        │
              │  (HTTP / Console /    │
              │   Multi / Chaos)      │
              └───────────────────────┘
```

---

## Components and Interfaces

### Repository Interface (extended)

```go
type Repository interface {
    // Existing methods (unchanged signatures)
    Store(event *Event) error
    GetPendingEvents(limit int) ([]*Event, error)
    GetByID(id uuid.UUID) (*Event, error)
    UpdateStatus(id uuid.UUID, status Status, errorMessage *string) error
    MarkAsProcessing(id uuid.UUID) error
    IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error
    DeleteCompletedEvents(olderThan time.Time) (int64, error)

    // New methods
    StoreWithTx(tx *sql.Tx, event *Event) error
    StoreIfNotExists(event *Event) (inserted bool, err error)
    RecoverStuckEvents(olderThan time.Time) (int64, error)
    ListDeadLetterEvents(limit int) ([]*Event, error)
    RequeueEvent(id uuid.UUID) error
}
```

### Event struct (extended)

```go
type Event struct {
    // ... existing fields ...
    DedupeKey string `json:"dedupe_key" db:"dedupe_key"`
}
```

### DispatcherConfig (extended)

```go
type DispatcherConfig struct {
    // ... existing fields ...
    RetryBaseDelay time.Duration // default 1s
}
```

### ServiceConfig (extended)

```go
type ServiceConfig struct {
    // ... existing fields ...
    PIIFieldBlocklist []string // default: ["email","phone","ssn","card_number","password","token","secret"]
}
```

### New functions

```go
// Pure backoff calculation — independently testable
func CalculateNextRetry(retryCount int, cfg DispatcherConfig) time.Duration

// PII sanitization
func SanitizePayload(data interface{}, blocklist []string) (json.RawMessage, error)
```

### Chaos test types (internal/outbox/chaos_test.go)

```go
type FaultType string
const (
    FaultError FaultType = "error"
    FaultDelay FaultType = "delay"
    FaultPanic FaultType = "panic"
)

type FaultSpec struct {
    Type        FaultType
    Probability float64       // 0.0–1.0
    MaxCount    int           // 0 = unlimited
    Delay       time.Duration // used when Type == FaultDelay
    Err         error         // used when Type == FaultError
}

type ChaosPublisher struct { /* wraps Publisher, applies FaultSpec */ }
type ChaosRepository struct { /* wraps Repository, applies per-method FaultSpecs */ }
```

---

## Data Models

### outbox_events schema changes

```sql
ALTER TABLE outbox_events
    ADD COLUMN dedupe_key VARCHAR(255);

CREATE UNIQUE INDEX idx_outbox_events_dedupe_key
    ON outbox_events(dedupe_key)
    WHERE dedupe_key IS NOT NULL;
```

The `dedupe_key` is nullable to preserve backward compatibility with events created before this migration. New events always have a non-null `dedupe_key`.

### DedupeKey generation (default)

When no explicit key is supplied:

```
dedupe_key = sha256(event_type + ":" + aggregate_id + ":" + occurred_at.UnixNano())
```

This is deterministic for a given business operation but unique across time, preventing false duplicate suppression for legitimately repeated events.

### Migration file

`migrations/002_outbox_hardening.up.sql` — adds `dedupe_key` column, unique index, and a composite index on `(status, updated_at)` to support `RecoverStuckEvents` efficiently.

---

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system — essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

### Property 1: Transactional atomicity on commit

*For any* event stored via `StoreWithTx` inside a transaction that is subsequently committed, the event row SHALL exist in the database with the correct `dedupe_key` and `status = 'pending'`.

**Validates: Requirements 1.1, 1.2**

---

### Property 2: Transactional atomicity on rollback

*For any* event stored via `StoreWithTx` inside a transaction that is subsequently rolled back, no row with that event's ID or `dedupe_key` SHALL exist in the database.

**Validates: Requirements 1.4**

---

### Property 3: DedupeKey determinism

*For any* two calls to `NewEvent` with identical `event_type`, `aggregate_id`, and `occurred_at`, the resulting `DedupeKey` values SHALL be equal.

**Validates: Requirements 2.2**

---

### Property 4: Explicit DedupeKey passthrough

*For any* non-empty string supplied as an explicit `DedupeKey`, the event created by `NewEvent` SHALL store that exact string in `DedupeKey` without modification.

**Validates: Requirements 2.3**

---

### Property 5: StoreIfNotExists idempotence

*For any* event, calling `StoreIfNotExists` twice with the same `dedupe_key` SHALL return `(true, nil)` on the first call and `(false, nil)` on the second call, with no error on either call.

**Validates: Requirements 2.4, 2.5**

---

### Property 6: Stuck-event recovery resets all eligible events

*For any* set of events in `processing` status with `updated_at` older than the `ProcessingTimeout` threshold, after one call to `RecoverStuckEvents(threshold)` every such event SHALL have `status = 'pending'` and an incremented `retry_count`.

**Validates: Requirements 3.1, 3.2, 3.3**

---

### Property 7: RecoverStuckEvents called every poll cycle

*For any* N poll cycles completed by a running Dispatcher, `RecoverStuckEvents` SHALL have been called exactly N times.

**Validates: Requirements 3.5**

---

### Property 8: Backoff duration is positive and bounded

*For any* `retry_count` in `[0, MaxRetries)`, `CalculateNextRetry(retryCount, cfg)` SHALL return a duration `d` such that:
- `d >= cfg.RetryBaseDelay * cfg.RetryBackoffFactor^retryCount`
- `d < cfg.RetryBaseDelay * cfg.RetryBackoffFactor^retryCount + cfg.RetryBaseDelay`

**Validates: Requirements 4.1, 4.4**

---

### Property 9: Backoff is monotonically non-decreasing (ignoring jitter)

*For any* `retry_count` values `a < b`, the lower bound of `CalculateNextRetry(a, cfg)` SHALL be less than or equal to the lower bound of `CalculateNextRetry(b, cfg)`.

**Validates: Requirements 4.1, 4.4**

---

### Property 10: SanitizePayload redacts all blocklist fields

*For any* JSON-serializable map containing one or more keys from the PII blocklist, `SanitizePayload` SHALL return a `json.RawMessage` where every blocklist key has the value `"[REDACTED]"` and all non-blocklist keys retain their original values.

**Validates: Requirements 5.1, 5.4**

---

### Property 11: NewEvent applies sanitization

*For any* event data map containing a PII field key, the `event_data` stored in the resulting `Event` SHALL not contain the original PII value.

**Validates: Requirements 5.2**

---

### Property 12: Dispatcher logs do not contain event_data

*For any* event processing error logged by the Dispatcher, the log output SHALL contain `event.ID` and `event.EventType` but SHALL NOT contain any substring of `event_data`.

**Validates: Requirements 5.3**

---

### Property 13: ChaosPublisher injects fault at probability 1.0

*For any* `FaultSpec` with `Probability = 1.0` and `Type = FaultError`, every call to `ChaosPublisher.Publish` SHALL return the configured error.

**Validates: Requirements 6.2**

---

### Property 14: Retry storm prevention via jitter spread

*For any* batch of 50 events all failing simultaneously, the standard deviation of their `next_retry_at` values SHALL be at least `RetryBaseDelay / 2`.

**Validates: Requirements 6.6**

---

### Property 15: ListDeadLetterEvents returns only exhausted-failed events

*For any* database state, `ListDeadLetterEvents` SHALL return only events where `status = 'failed'` AND `retry_count >= max_retries`, ordered by `updated_at ASC`.

**Validates: Requirements 7.1**

---

### Property 16: RequeueEvent round-trip

*For any* dead-lettered event (status=failed, retry_count>=max_retries), after calling `RequeueEvent`, the event SHALL have `status = 'pending'`, `retry_count = 0`, and `next_retry_at = NULL`.

**Validates: Requirements 7.2**

---

## Error Handling

| Scenario | Behavior |
|---|---|
| `StoreWithTx` called with nil `*sql.Tx` | Return `ErrNilTransaction` immediately |
| `StoreIfNotExists` duplicate key | Return `(false, nil)` — not an error |
| `RecoverStuckEvents` DB error | Log error, return 0, continue poll cycle |
| Publisher returns error | Increment retry, schedule backoff |
| Publisher panics | `recover()` in dispatcher goroutine, treat as error, increment retry |
| `RequeueEvent` on non-failed event | Return `ErrEventNotDeadLettered` |
| `SanitizePayload` on non-map JSON | Return input unchanged (best-effort) |

---

## Testing Strategy

### Unit tests (no DB required)

- `CalculateNextRetry`: property tests via `testing/quick` — positive, bounded, monotone.
- `SanitizePayload`: property tests — all blocklist keys redacted, non-blocklist keys preserved.
- `ChaosPublisher` / `ChaosRepository`: unit tests for fault injection logic.
- `NewEvent` with PII data: assert sanitization applied.
- `DedupeKey` generation: determinism and passthrough properties.

### Integration tests (testcontainers-go PostgreSQL)

All integration tests spin up a real PostgreSQL container via `testcontainers-go` (already in `go.mod`). No pre-existing local database is required.

- `TestStoreWithTx_Commit`: Property 1
- `TestStoreWithTx_Rollback`: Property 2
- `TestStoreIfNotExists_Idempotence`: Property 5
- `TestRecoverStuckEvents`: Property 6
- `TestProcessCrashMidFlight`: Requirement 6.4
- `TestDBFailoverRecovery`: Requirement 6.5
- `TestRetryStormPrevention`: Property 14
- `TestPanicRecovery`: Requirement 6.7
- `TestListDeadLetterEvents`: Property 15
- `TestRequeueEvent`: Property 16

### Property-based tests

Use `testing/quick` (stdlib) for pure functions:
- `CalculateNextRetry` — Properties 8, 9
- `SanitizePayload` — Property 10
- `DedupeKey` generation — Properties 3, 4

### Coverage target

`go test -cover ./internal/outbox/...` must report ≥ 95% statement coverage.

### Test tagging

Each test function includes a comment:
```go
// Feature: outbox-hardening, Property N: <property text>
```
