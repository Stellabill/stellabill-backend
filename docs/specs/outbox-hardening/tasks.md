# Implementation Plan: Outbox Hardening

## Overview

Incremental implementation of exactly-once publish semantics, jitter backoff, PII sanitization, chaos tests, dead-letter observability, and operational runbook for the Stellabill outbox package.

## Tasks

- [x] 1. Database migration — add dedupe_key column and supporting indexes
  - Create `migrations/002_outbox_hardening.up.sql` with `ALTER TABLE outbox_events ADD COLUMN dedupe_key VARCHAR(255)`, a partial unique index on `dedupe_key WHERE dedupe_key IS NOT NULL`, and a composite index on `(status, updated_at)` for `RecoverStuckEvents`.
  - Create `migrations/002_outbox_hardening.down.sql` that drops the column and indexes.
  - _Requirements: 2.1, 2.6, 3.3_

- [x] 2. Extend Event struct and NewEvent with DedupeKey
  - [x] 2.1 Add `DedupeKey string` field to `Event` in `internal/outbox/types.go` with `json:"dedupe_key"` and `db:"dedupe_key"` tags.
    - _Requirements: 2.1_

  - [x] 2.2 Add `CalculateNextRetry(retryCount int, cfg DispatcherConfig) time.Duration` pure function in a new file `internal/outbox/backoff.go`.
    - Formula: `base * factor^retryCount + uniform_jitter([0, base))`.
    - Add `RetryBaseDelay time.Duration` (default `1s`) to `DispatcherConfig` and `DefaultDispatcherConfig()`.
    - _Requirements: 4.1, 4.2, 4.4_

  - [ ]* 2.3 Write property tests for CalculateNextRetry using `testing/quick`
    - **Property 8: Backoff duration is positive and bounded**
    - **Property 9: Backoff is monotonically non-decreasing**
    - **Validates: Requirements 4.1, 4.4**

  - [x] 2.4 Add `SanitizePayload(data interface{}, blocklist []string) (json.RawMessage, error)` in `internal/outbox/sanitize.go`.
    - Walk top-level JSON keys; replace blocklist matches with `"[REDACTED]"`.
    - Add `PIIFieldBlocklist []string` to `ServiceConfig` with secure defaults.
    - _Requirements: 5.1, 5.5_

  - [ ]* 2.5 Write property tests for SanitizePayload using `testing/quick`
    - **Property 10: SanitizePayload redacts all blocklist fields**
    - **Validates: Requirements 5.1, 5.4**

  - [x] 2.6 Update `NewEvent` in `repository.go` to: (a) call `SanitizePayload` before marshalling, (b) generate default `DedupeKey` as `sha256(event_type + ":" + aggregate_id + ":" + occurred_at.UnixNano())`, (c) accept an optional explicit `DedupeKey` parameter.
    - _Requirements: 2.2, 2.3, 5.2_

  - [ ]* 2.7 Write property tests for DedupeKey generation
    - **Property 3: DedupeKey determinism**
    - **Property 4: Explicit DedupeKey passthrough**
    - **Property 11: NewEvent applies sanitization**
    - **Validates: Requirements 2.2, 2.3, 5.2**

- [x] 3. Extend Repository with new methods
  - [x] 3.1 Implement `StoreWithTx(tx *sql.Tx, event *Event) error` in `repository.go`.
    - Use `tx.Exec` instead of `r.db.Exec`; return `ErrNilTransaction` if `tx == nil`.
    - Include `dedupe_key` in the INSERT column list.
    - _Requirements: 1.1, 1.3_

  - [x] 3.2 Implement `StoreIfNotExists(event *Event) (bool, error)` in `repository.go`.
    - Use `INSERT … ON CONFLICT (dedupe_key) DO NOTHING` and check `RowsAffected`.
    - _Requirements: 2.4, 2.5_

  - [ ]* 3.3 Write property test for StoreIfNotExists idempotence (requires testcontainers-go)
    - **Property 5: StoreIfNotExists idempotence**
    - **Validates: Requirements 2.4, 2.5**

  - [x] 3.4 Implement `RecoverStuckEvents(olderThan time.Time) (int64, error)` in `repository.go`.
    - Single `UPDATE outbox_events SET status='pending', retry_count=retry_count+1, updated_at=NOW() WHERE status='processing' AND updated_at < $1`.
    - _Requirements: 3.3_

  - [ ]* 3.5 Write property test for RecoverStuckEvents (requires testcontainers-go)
    - **Property 6: Stuck-event recovery resets all eligible events**
    - **Validates: Requirements 3.1, 3.2, 3.3**

  - [x] 3.6 Implement `ListDeadLetterEvents(limit int) ([]*Event, error)` in `repository.go`.
    - `SELECT … WHERE status='failed' AND retry_count >= max_retries ORDER BY updated_at ASC LIMIT $1`.
    - _Requirements: 7.1_

  - [x] 3.7 Implement `RequeueEvent(id uuid.UUID) error` in `repository.go`.
    - `UPDATE … SET status='pending', retry_count=0, next_retry_at=NULL WHERE id=$1 AND status='failed'`; return `ErrEventNotDeadLettered` if `RowsAffected == 0`.
    - _Requirements: 7.2, 7.3_

  - [ ]* 3.8 Write property tests for ListDeadLetterEvents and RequeueEvent (requires testcontainers-go)
    - **Property 15: ListDeadLetterEvents returns only exhausted-failed events**
    - **Property 16: RequeueEvent round-trip**
    - **Validates: Requirements 7.1, 7.2**

  - [x] 3.9 Update all existing `Store` and `GetPendingEvents` SQL in `repository.go` to include the `dedupe_key` column in SELECT and INSERT lists.
    - _Requirements: 2.1_

- [x] 4. Checkpoint — ensure all unit and property tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 5. Update Dispatcher to use new repository methods and safe logging
  - [x] 5.1 Add `RecoverStuckEvents` call at the top of `processPendingEvents()` in `dispatcher.go`, before fetching pending events. Log error and continue on failure.
    - _Requirements: 3.1, 3.4, 3.5_

  - [ ]* 5.2 Write property test for RecoverStuckEvents called every poll cycle using a spy repository
    - **Property 7: RecoverStuckEvents called every poll cycle**
    - **Validates: Requirements 3.5**

  - [x] 5.3 Replace `handlePublishError` backoff calculation with `CalculateNextRetry` in `dispatcher.go`.
    - _Requirements: 4.1, 4.3_

  - [x] 5.4 Add `recover()` wrapper around the `publisher.Publish(event)` goroutine in `processEvent` to catch panics; treat recovered panics as publish errors.
    - _Requirements: 6.7_

  - [x] 5.5 Update all `log.Printf` calls in `dispatcher.go` that reference event data to log only `event.ID`, `event.EventType`, and `retry_count`.
    - _Requirements: 5.3_

  - [ ]* 5.6 Write property test asserting Dispatcher logs do not contain event_data
    - **Property 12: Dispatcher logs do not contain event_data**
    - **Validates: Requirements 5.3**

- [x] 6. Fix PublishEventWithTx to use StoreWithTx
  - Update `Service.PublishEventWithTx` in `service.go` to call `s.repository.StoreWithTx(tx, event)` instead of `s.repository.Store(event)`.
  - Expose `ListDeadLetterEvents` and `RequeueEvent` on `Service` delegating to the repository.
  - _Requirements: 1.2, 7.4_

  - [ ]* 6.1 Write integration tests for transactional atomicity (requires testcontainers-go)
    - **Property 1: Transactional atomicity on commit**
    - **Property 2: Transactional atomicity on rollback**
    - **Validates: Requirements 1.1, 1.2, 1.4**

- [x] 7. Implement chaos test infrastructure
  - [x] 7.1 Create `internal/outbox/chaos_test.go` with `FaultSpec`, `ChaosPublisher`, and `ChaosRepository` types.
    - `ChaosPublisher` wraps a `Publisher`; on each `Publish` call, evaluates `FaultSpec` (probability check, decrement MaxCount) and injects error/delay/panic accordingly.
    - `ChaosRepository` wraps a `Repository`; accepts per-method `FaultSpec` maps.
    - _Requirements: 6.1, 6.2, 6.3_

  - [ ]* 7.2 Write property test for ChaosPublisher fault injection at probability 1.0
    - **Property 13: ChaosPublisher injects fault at probability 1.0**
    - **Validates: Requirements 6.2**

  - [x] 7.3 Write `TestProcessCrashMidFlight` integration test using testcontainers-go.
    - Store event → mark processing → stop Dispatcher → restart Dispatcher → assert `completed`.
    - _Requirements: 6.4_

  - [x] 7.4 Write `TestDBFailoverRecovery` integration test using `ChaosRepository`.
    - Fail all DB calls for 3 poll cycles → restore → assert all pending events reach `completed`.
    - _Requirements: 6.5_

  - [x] 7.5 Write `TestRetryStormPrevention` test.
    - Create 50 events, use `ChaosPublisher` with `Probability=1.0, Type=FaultError` for first attempt, collect `next_retry_at` values, assert stddev >= `RetryBaseDelay/2`.
    - _Requirements: 6.6_

  - [x] 7.6 Write `TestPanicRecovery` test.
    - Use `ChaosPublisher` with `Type=FaultPanic, Probability=1.0, MaxCount=1` for first event; assert Dispatcher continues and subsequent events reach `completed`.
    - _Requirements: 6.7_

- [x] 8. Checkpoint — ensure all integration and chaos tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 9. Write operational runbook
  - Create `docs/runbooks/outbox-operations.md` covering: stuck events in `processing`, dead-lettered events, high queue depth, and database connectivity loss.
  - Each section: symptoms, diagnostic SQL with `psql` examples, recovery steps using `RequeueEvent` API and `RecoverStuckEvents`, prevention measures.
  - _Requirements: 8.1, 8.2, 8.3, 8.4_

- [x] 10. Final checkpoint — verify coverage and documentation
  - Run `go test -cover ./internal/outbox/...` and confirm ≥ 95% statement coverage.
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for a faster MVP.
- All integration tests use `testcontainers-go` — no local PostgreSQL required.
- Each property test references its design document property number in a comment.
- `CalculateNextRetry` must be a pure function (no side effects) to enable `testing/quick` property tests.
- The `dedupe_key` column is nullable for backward compatibility; new events always populate it.
