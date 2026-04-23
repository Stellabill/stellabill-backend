# Requirements Document

## Introduction

This feature hardens the existing outbox pattern implementation in Stellabill to provide exactly-once publish semantics (at-least-once delivery with idempotent downstream processing), robust retry/backoff, failure injection tests, and operational runbooks for stuck events. The current implementation in `internal/outbox/` has the core structure in place but lacks deduplicate keys, transactional write guarantees enforced at the repository level, processing-state timeout recovery, chaos/failure injection tests, and PII-safe payload handling.

## Glossary

- **Outbox**: The `outbox_events` PostgreSQL table and the Go package `internal/outbox/` that manages reliable event publication.
- **Dispatcher**: The background goroutine (`dispatcher.go`) that polls for pending events and publishes them.
- **Repository**: The `postgresRepository` struct that performs all SQL operations against `outbox_events`.
- **Publisher**: Any implementation of the `Publisher` interface (console, HTTP, multi).
- **Dedupe_Key**: A caller-supplied idempotency key stored on an outbox event to allow downstream consumers to detect and discard duplicate deliveries.
- **Processing_Timeout**: The maximum wall-clock duration an event may remain in `processing` status before the Dispatcher treats it as stuck and resets it to `pending`.
- **Chaos_Injector**: A test-only `Publisher` or `Repository` wrapper that injects configurable faults (errors, delays, panics) to simulate failure scenarios.
- **PII**: Personally Identifiable Information — data that must not appear in outbox payloads or log lines.
- **Dead_Letter**: The terminal `failed` status reached when an event exhausts `max_retries`.
- **Runbook**: Operational documentation describing how to diagnose and recover from a specific failure mode.

## Requirements

### Requirement 1: Transactional Outbox Write Guarantee

**User Story:** As a backend engineer, I want every outbox event write to be part of the same database transaction as the business data mutation, so that events are never lost or orphaned when a transaction rolls back.

#### Acceptance Criteria

1. THE Repository SHALL expose a `StoreWithTx(tx *sql.Tx, event *Event) error` method that writes the event row within the caller-supplied transaction.
2. WHEN a caller invokes `Service.PublishEventWithTx`, THE Service SHALL use `StoreWithTx` rather than opening a new connection, ensuring the event row is committed or rolled back atomically with the surrounding business transaction.
3. IF `StoreWithTx` is called outside an active transaction, THEN THE Repository SHALL return a descriptive error without writing any data.
4. WHEN the surrounding business transaction is rolled back, THE Repository SHALL ensure no orphaned outbox row exists for that event.

---

### Requirement 2: Deduplicate Keys for Exactly-Once Processing

**User Story:** As a backend engineer, I want each outbox event to carry a stable deduplicate key, so that downstream consumers can detect and discard duplicate deliveries caused by retries.

#### Acceptance Criteria

1. THE Event struct SHALL include a `DedupeKey` field (type `string`, database column `dedupe_key VARCHAR(255)`).
2. WHEN a new event is created via `NewEvent`, THE Outbox SHALL populate `DedupeKey` with a deterministic value derived from `event_type`, `aggregate_id`, and `occurred_at` unless the caller supplies an explicit key.
3. WHERE a caller supplies an explicit `DedupeKey`, THE Outbox SHALL store the caller-supplied value unchanged.
4. THE Repository SHALL expose a `StoreIfNotExists(event *Event) (bool, error)` method that inserts the event only when no row with the same `dedupe_key` already exists, returning `(true, nil)` on insert and `(false, nil)` on duplicate.
5. WHEN `StoreIfNotExists` detects a duplicate `dedupe_key`, THE Repository SHALL NOT return an error; it SHALL return `(false, nil)`.
6. THE migration SHALL add a `UNIQUE` constraint on `dedupe_key` and a supporting index.

---

### Requirement 3: Processing-State Timeout Recovery

**User Story:** As a backend engineer, I want events stuck in `processing` status to be automatically recovered, so that a process crash mid-flight does not permanently block event delivery.

#### Acceptance Criteria

1. THE Dispatcher SHALL query for events in `processing` status whose `updated_at` is older than `ProcessingTimeout` and reset them to `pending` status during each poll cycle.
2. WHEN an event is reset from `processing` to `pending`, THE Dispatcher SHALL increment `retry_count` and set `next_retry_at` using the configured exponential backoff formula.
3. THE Repository SHALL expose a `RecoverStuckEvents(olderThan time.Time) (int64, error)` method that performs the reset atomically using a single `UPDATE … WHERE status = 'processing' AND updated_at < $1` statement.
4. IF `RecoverStuckEvents` fails due to a database error, THEN THE Dispatcher SHALL log the error and continue the poll cycle without crashing.
5. WHILE the Dispatcher is running, THE Dispatcher SHALL call `RecoverStuckEvents` on every poll cycle before fetching new pending events.

---

### Requirement 4: Exponential Backoff with Jitter

**User Story:** As a backend engineer, I want retry delays to include jitter, so that multiple failing events do not create synchronized retry storms against the downstream publisher.

#### Acceptance Criteria

1. WHEN calculating `next_retry_at` after a publish failure, THE Dispatcher SHALL apply the formula: `base_delay * backoff_factor^retry_count + random_jitter` where `random_jitter` is uniformly distributed in `[0, base_delay)`.
2. THE `DispatcherConfig` SHALL include a `RetryBaseDelay time.Duration` field (default `1s`) used as the base for backoff calculations.
3. WHEN `retry_count` reaches `MaxRetries`, THE Dispatcher SHALL transition the event to `failed` status and SHALL NOT schedule further retries.
4. THE backoff calculation SHALL be encapsulated in a pure function `CalculateNextRetry(retryCount int, cfg DispatcherConfig) time.Duration` that is independently testable.

---

### Requirement 5: PII-Safe Payloads and Logs

**User Story:** As a security engineer, I want outbox payloads and log lines to be free of PII, so that sensitive customer data is not persisted in the outbox table or emitted to log aggregators.

#### Acceptance Criteria

1. THE Outbox SHALL provide a `SanitizePayload(data interface{}) (json.RawMessage, error)` function that redacts fields whose JSON key matches a configurable blocklist (e.g., `email`, `phone`, `ssn`, `card_number`, `password`).
2. WHEN `NewEvent` is called, THE Outbox SHALL apply `SanitizePayload` to the event data before marshalling it into `event_data`.
3. WHEN the Dispatcher logs an event processing error, THE Dispatcher SHALL log only `event.ID`, `event.EventType`, and `retry_count`; it SHALL NOT log `event_data` contents.
4. IF `SanitizePayload` encounters a field on the blocklist, THEN THE Outbox SHALL replace the field value with the string `"[REDACTED]"`.
5. THE blocklist SHALL be configurable via `ServiceConfig.PIIFieldBlocklist []string` with a secure default set of `["email", "phone", "ssn", "card_number", "password", "token", "secret"]`.

---

### Requirement 6: Failure Injection (Chaos) Tests

**User Story:** As a backend engineer, I want a suite of failure injection tests, so that I can verify the outbox system behaves correctly under process crashes, database failovers, and retry storms.

#### Acceptance Criteria

1. THE test suite SHALL include a `ChaosPublisher` type that accepts a `FaultSpec` describing fault type (error, delay, panic), probability (0.0–1.0), and maximum fault count.
2. WHEN `ChaosPublisher.Publish` is called and the fault condition triggers, THE ChaosPublisher SHALL inject the configured fault (return error, sleep for delay duration, or call `recover`-safe panic).
3. THE test suite SHALL include a `ChaosRepository` type that wraps a real `Repository` and can inject faults on `Store`, `MarkAsProcessing`, `UpdateStatus`, and `RecoverStuckEvents`.
4. THE test suite SHALL include a test named `TestProcessCrashMidFlight` that: stores an event, marks it as processing, simulates a crash by stopping the Dispatcher without completing the event, restarts the Dispatcher, and asserts the event reaches `completed` status.
5. THE test suite SHALL include a test named `TestDBFailoverRecovery` that: uses `ChaosRepository` to fail all DB calls for a configurable window, then restores DB access, and asserts all pending events are eventually published.
6. THE test suite SHALL include a test named `TestRetryStormPrevention` that: creates 50 events all failing simultaneously, and asserts that the spread of `next_retry_at` values across events has a standard deviation of at least `RetryBaseDelay / 2` (demonstrating jitter).
7. WHEN a `ChaosPublisher` panic fault triggers, THE Dispatcher SHALL recover from the panic via `recover()` and continue processing remaining events.

---

### Requirement 7: Dead-Letter Queue Observability

**User Story:** As an operator, I want to query and requeue dead-lettered events, so that I can recover from permanent failures without manual SQL.

#### Acceptance Criteria

1. THE Repository SHALL expose a `ListDeadLetterEvents(limit int) ([]*Event, error)` method that returns events with `status = 'failed'` and `retry_count >= max_retries`, ordered by `updated_at ASC`.
2. THE Repository SHALL expose a `RequeueEvent(id uuid.UUID) error` method that resets a dead-lettered event to `status = 'pending'`, `retry_count = 0`, and `next_retry_at = NULL`.
3. WHEN `RequeueEvent` is called on an event that is not in `failed` status, THE Repository SHALL return a descriptive error without modifying the event.
4. THE Service SHALL expose `ListDeadLetterEvents` and `RequeueEvent` methods that delegate to the Repository.

---

### Requirement 8: Operational Runbook

**User Story:** As an operator, I want documented runbooks for common outbox failure modes, so that I can diagnose and recover from incidents without deep code knowledge.

#### Acceptance Criteria

1. THE codebase SHALL include a file `docs/runbooks/outbox-operations.md` covering at minimum: stuck events in `processing`, dead-lettered events, high queue depth, and database connectivity loss.
2. WHEN describing each failure mode, THE Runbook SHALL include: symptoms, diagnostic SQL queries, recovery steps, and prevention measures.
3. THE Runbook SHALL document the `RequeueEvent` API and the `RecoverStuckEvents` mechanism as recovery tools.
4. THE Runbook SHALL include example `psql` commands for each diagnostic query.

---

### Requirement 9: Test Coverage

**User Story:** As a backend engineer, I want the outbox package to maintain at least 95% statement coverage, so that regressions are caught early.

#### Acceptance Criteria

1. WHEN running `go test -cover ./internal/outbox/...`, THE test suite SHALL report statement coverage of at least 95%.
2. THE test suite SHALL include property-based tests using `pgx` or `testing/quick` for the `CalculateNextRetry` function verifying that returned durations are always positive and increase monotonically with `retry_count`.
3. THE test suite SHALL use `testcontainers-go` (already in `go.mod`) for integration tests that require a real PostgreSQL instance, avoiding reliance on a pre-existing local database.
4. WHEN any test fails, THE test output SHALL clearly identify which requirement acceptance criterion the failure relates to via a test name or comment.
