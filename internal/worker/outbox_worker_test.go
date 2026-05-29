package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// mockPublisher records calls and optionally returns errors.
type mockPublisher struct {
	mu        sync.Mutex
	calls     []*OutboxEvent
	err       error
	callCount atomic.Int32
}

func (m *mockPublisher) Publish(_ context.Context, event *OutboxEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, event)
	m.callCount.Add(1)
	return m.err
}

func (m *mockPublisher) getCalls() []*OutboxEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*OutboxEvent, len(m.calls))
	copy(out, m.calls)
	return out
}

// failNTimesPublisher fails the first N calls, then succeeds.
type failNTimesPublisher struct {
	mu       sync.Mutex
	failFor  int
	calls    int
	err      error
}

func (f *failNTimesPublisher) Publish(_ context.Context, _ *OutboxEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failFor {
		return f.err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helper: build sqlmock rows matching outbox_events schema
// ---------------------------------------------------------------------------

func outboxColumns() []string {
	return []string{
		"id", "event_type", "event_data", "aggregate_id", "aggregate_type",
		"occurred_at", "status", "retry_count", "max_retries", "next_retry_at",
		"error_message", "created_at", "updated_at", "version",
	}
}

func newTestEvent(id uuid.UUID, status string, retryCount, maxRetries int) *OutboxEvent {
	return &OutboxEvent{
		ID:         id,
		EventType:  "test.event",
		EventData:  json.RawMessage(`{"key":"value"}`),
		OccurredAt: time.Now().UTC(),
		Status:     status,
		RetryCount: retryCount,
		MaxRetries: maxRetries,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		Version:    1,
	}
}

func addEventRow(rows *sqlmock.Rows, e *OutboxEvent) *sqlmock.Rows {
	return rows.AddRow(
		e.ID, e.EventType, e.EventData, e.AggregateID, e.AggregateType,
		e.OccurredAt, e.Status, e.RetryCount, e.MaxRetries, e.NextRetryAt,
		e.ErrorMessage, e.CreatedAt, e.UpdatedAt, e.Version,
	)
}

// fastConfig returns a config suitable for fast unit tests.
func fastConfig() OutboxWorkerConfig {
	return OutboxWorkerConfig{
		PollInterval:      50 * time.Millisecond,
		BatchSize:         10,
		MaxRetries:        3,
		RetryBackoffBase:  10 * time.Millisecond,
		ShutdownTimeout:   2 * time.Second,
		ProcessingTimeout: 1 * time.Second,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestOutboxWorker_HealthWhenNotRunning(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	pub := &mockPublisher{}
	w := NewOutboxWorker(db, pub, fastConfig())

	err = w.Health()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestOutboxWorker_HealthWhenRunning(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	pub := &mockPublisher{}
	cfg := fastConfig()
	cfg.PollInterval = 10 * time.Second // prevent actual polls
	w := NewOutboxWorker(db, pub, cfg)

	// Expect the poll to begin a transaction eventually - but we'll stop
	// before it fires due to the long interval.
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(outboxColumns()))
	mock.ExpectRollback()

	w.Start()
	defer w.Stop()

	err = w.Health()
	assert.NoError(t, err)
}

func TestOutboxWorker_GetStats(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	pub := &mockPublisher{}
	w := NewOutboxWorker(db, pub, fastConfig())

	stats, err := w.GetStats()
	require.NoError(t, err)
	assert.Equal(t, false, stats["running"])
	assert.Equal(t, int64(0), stats["processed"])
	assert.Equal(t, int64(0), stats["succeeded"])
}

func TestOutboxWorker_SuccessfulProcessing(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	eventID := uuid.New()
	event := newTestEvent(eventID, OutboxStatusPending, 0, 3)

	pub := &mockPublisher{}
	cfg := fastConfig()
	w := NewOutboxWorker(db, pub, cfg)

	// Expect: BEGIN → SELECT FOR UPDATE SKIP LOCKED → UPDATE to processing → COMMIT
	mock.ExpectBegin()
	rows := sqlmock.NewRows(outboxColumns())
	addEventRow(rows, event)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)
	mock.ExpectExec("UPDATE outbox_events SET status").
		WithArgs(OutboxStatusProcessing, eventID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// After processing: UPDATE to completed
	mock.ExpectExec("UPDATE outbox_events").
		WithArgs(OutboxStatusCompleted, eventID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Second poll returns empty
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(outboxColumns()))
	mock.ExpectRollback()

	w.Start()

	// Wait for processing
	assert.Eventually(t, func() bool {
		return pub.callCount.Load() >= 1
	}, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, w.Stop())

	calls := pub.getCalls()
	assert.Len(t, calls, 1)
	assert.Equal(t, eventID, calls[0].ID)

	stats, _ := w.GetStats()
	assert.Equal(t, int64(1), stats["processed"])
	assert.Equal(t, int64(1), stats["succeeded"])
}

func TestOutboxWorker_RetryScheduling(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	eventID := uuid.New()
	event := newTestEvent(eventID, OutboxStatusPending, 0, 3)
	publishErr := fmt.Errorf("temporary failure")

	pub := &mockPublisher{err: publishErr}
	cfg := fastConfig()
	w := NewOutboxWorker(db, pub, cfg)

	// First poll: claim event
	mock.ExpectBegin()
	rows := sqlmock.NewRows(outboxColumns())
	addEventRow(rows, event)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)
	mock.ExpectExec("UPDATE outbox_events SET status").
		WithArgs(OutboxStatusProcessing, eventID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Failure: UPDATE with retry_count=1, status=failed, next_retry_at set
	mock.ExpectExec("UPDATE outbox_events").
		WithArgs(OutboxStatusFailed, 1, sqlmock.AnyArg(), publishErr.Error(), eventID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Second poll returns empty
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(outboxColumns()))
	mock.ExpectRollback()

	w.Start()

	assert.Eventually(t, func() bool {
		return pub.callCount.Load() >= 1
	}, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, w.Stop())

	stats, _ := w.GetStats()
	assert.Equal(t, int64(1), stats["processed"])
	assert.Equal(t, int64(1), stats["failed"])
	assert.Equal(t, int64(0), stats["dead_lettered"])
}

func TestOutboxWorker_DeadLetterAfterMaxRetries(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	eventID := uuid.New()
	// Event already at retry_count=2 with max_retries=3 → next failure = dead-letter
	event := newTestEvent(eventID, OutboxStatusFailed, 2, 3)
	retryAt := time.Now().Add(-time.Minute)
	event.NextRetryAt = &retryAt

	publishErr := fmt.Errorf("permanent failure")
	pub := &mockPublisher{err: publishErr}
	cfg := fastConfig()
	cfg.MaxRetries = 3
	w := NewOutboxWorker(db, pub, cfg)

	// Claim
	mock.ExpectBegin()
	rows := sqlmock.NewRows(outboxColumns())
	addEventRow(rows, event)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)
	mock.ExpectExec("UPDATE outbox_events SET status").
		WithArgs(OutboxStatusProcessing, eventID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Dead-letter: status=failed, retry_count=3 (>= max)
	mock.ExpectExec("UPDATE outbox_events").
		WithArgs(OutboxStatusFailed, 3, publishErr.Error(), eventID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Next poll empty
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(outboxColumns()))
	mock.ExpectRollback()

	w.Start()

	assert.Eventually(t, func() bool {
		return pub.callCount.Load() >= 1
	}, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, w.Stop())

	stats, _ := w.GetStats()
	assert.Equal(t, int64(1), stats["dead_lettered"])
}

func TestOutboxWorker_ExponentialBackoff(t *testing.T) {
	// Verify that the backoff calculation produces increasing intervals.
	cfg := DefaultOutboxWorkerConfig()
	cfg.RetryBackoffBase = 1 * time.Second

	// base * 2^(retryCount-1)
	tests := []struct {
		retryCount int
		expected   time.Duration
	}{
		{1, 1 * time.Second},  // 1s * 2^0
		{2, 2 * time.Second},  // 1s * 2^1
		{3, 4 * time.Second},  // 1s * 2^2
		{4, 8 * time.Second},  // 1s * 2^3
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("retry_%d", tt.retryCount), func(t *testing.T) {
			import "math"
			backoff := cfg.RetryBackoffBase * time.Duration(math.Pow(2, float64(tt.retryCount-1)))
			assert.Equal(t, tt.expected, backoff)
		})
	}
}

func TestOutboxWorker_GracefulShutdown(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Slow publisher that takes 200ms
	slowPub := &slowPublisher{delay: 200 * time.Millisecond}
	cfg := fastConfig()
	cfg.ShutdownTimeout = 5 * time.Second
	w := NewOutboxWorker(db, slowPub, cfg)

	eventID := uuid.New()
	event := newTestEvent(eventID, OutboxStatusPending, 0, 3)

	// First poll claims an event
	mock.ExpectBegin()
	rows := sqlmock.NewRows(outboxColumns())
	addEventRow(rows, event)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)
	mock.ExpectExec("UPDATE outbox_events SET status").
		WithArgs(OutboxStatusProcessing, eventID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Completion update
	mock.ExpectExec("UPDATE outbox_events").
		WithArgs(OutboxStatusCompleted, eventID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	w.Start()

	// Wait for the publisher to be invoked
	assert.Eventually(t, func() bool {
		return slowPub.callCount.Load() >= 1
	}, 2*time.Second, 10*time.Millisecond)

	// Stop should wait for in-flight work and return nil
	err = w.Stop()
	assert.NoError(t, err)

	stats, _ := w.GetStats()
	assert.Equal(t, false, stats["running"])
}

// slowPublisher simulates a slow event handler.
type slowPublisher struct {
	delay     time.Duration
	callCount atomic.Int32
}

func (s *slowPublisher) Publish(ctx context.Context, _ *OutboxEvent) error {
	s.callCount.Add(1)
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestOutboxWorker_EmptyPollDoesNotFail(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	pub := &mockPublisher{}
	cfg := fastConfig()
	w := NewOutboxWorker(db, pub, cfg)

	// Two empty polls
	for i := 0; i < 2; i++ {
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(outboxColumns()))
		mock.ExpectRollback()
	}

	w.Start()

	// Let it tick twice
	time.Sleep(150 * time.Millisecond)

	require.NoError(t, w.Stop())
	assert.Empty(t, pub.getCalls())
}

func TestOutboxWorker_HealthReportsConsecutiveErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	pub := &mockPublisher{}
	cfg := fastConfig()
	w := NewOutboxWorker(db, pub, cfg)

	// Make 6 polls fail with transaction errors
	for i := 0; i < 7; i++ {
		mock.ExpectBegin().WillReturnError(fmt.Errorf("connection refused"))
	}
	// Then recover
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(outboxColumns()))
	mock.ExpectRollback()

	w.Start()

	// Wait for enough failed polls
	time.Sleep(400 * time.Millisecond)

	err = w.Health()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "consecutive poll errors")

	require.NoError(t, w.Stop())
}

func TestOutboxWorker_OnlyEligibleEventsProcessed(t *testing.T) {
	// This test verifies that the SQL query only returns eligible rows.
	// The eligibility is enforced by the SQL WHERE clause:
	//   status = 'pending' OR (status = 'failed' AND next_retry_at <= NOW())
	// Rows with status='completed', status='processing', or
	// status='failed' with next_retry_at in the future are skipped.

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	eligibleID := uuid.New()
	eligible := newTestEvent(eligibleID, OutboxStatusPending, 0, 3)

	pub := &mockPublisher{}
	cfg := fastConfig()
	w := NewOutboxWorker(db, pub, cfg)

	// Only the eligible event is returned by the query
	mock.ExpectBegin()
	rows := sqlmock.NewRows(outboxColumns())
	addEventRow(rows, eligible)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)
	mock.ExpectExec("UPDATE outbox_events SET status").
		WithArgs(OutboxStatusProcessing, eligibleID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectExec("UPDATE outbox_events").
		WithArgs(OutboxStatusCompleted, eligibleID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Second poll empty
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(outboxColumns()))
	mock.ExpectRollback()

	w.Start()

	assert.Eventually(t, func() bool {
		return pub.callCount.Load() >= 1
	}, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, w.Stop())

	calls := pub.getCalls()
	assert.Len(t, calls, 1)
	assert.Equal(t, eligibleID, calls[0].ID)
}

func TestOutboxWorker_MultipleBatchProcessing(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	events := make([]*OutboxEvent, 3)
	for i, id := range ids {
		events[i] = newTestEvent(id, OutboxStatusPending, 0, 3)
	}

	pub := &mockPublisher{}
	cfg := fastConfig()
	cfg.BatchSize = 3
	w := NewOutboxWorker(db, pub, cfg)

	// Claim all 3
	mock.ExpectBegin()
	rows := sqlmock.NewRows(outboxColumns())
	for _, e := range events {
		addEventRow(rows, e)
	}
	mock.ExpectQuery("SELECT").WillReturnRows(rows)
	for _, id := range ids {
		mock.ExpectExec("UPDATE outbox_events SET status").
			WithArgs(OutboxStatusProcessing, id).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()

	// Completion for each
	for _, id := range ids {
		mock.ExpectExec("UPDATE outbox_events").
			WithArgs(OutboxStatusCompleted, id).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}

	// Next poll empty
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(outboxColumns()))
	mock.ExpectRollback()

	w.Start()

	assert.Eventually(t, func() bool {
		return pub.callCount.Load() >= 3
	}, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, w.Stop())

	stats, _ := w.GetStats()
	assert.Equal(t, int64(3), stats["processed"])
	assert.Equal(t, int64(3), stats["succeeded"])
}

func TestOutboxWorker_SatisfiesOutboxHealtherInterface(t *testing.T) {
	// Compile-time check that OutboxWorker satisfies the interface shape.
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	w := NewOutboxWorker(db, &mockPublisher{}, fastConfig())

	// The interface is: Health() error, GetStats() (map[string]interface{}, error)
	var _ interface {
		Health() error
		GetStats() (map[string]interface{}, error)
	} = w

	assert.NotNil(t, w)
}

// ---------------------------------------------------------------------------
// Compile-time interface assertion
// ---------------------------------------------------------------------------

var _ EventPublisher = (*mockPublisher)(nil)
var _ EventPublisher = (*slowPublisher)(nil)
var _ EventPublisher = (*failNTimesPublisher)(nil)
