package worker_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"stellarbill-backend/internal/integrations/pagerduty"
	"stellarbill-backend/internal/worker"
)

// fakeAlertClient captures Trigger / Resolve calls.
type fakeAlertClient struct {
	triggerCalls int
	resolveCalls int
	triggerErr   error
	resolveErr   error
	lastSummary  string
	lastDedup    string
}

func (f *fakeAlertClient) Trigger(_ context.Context, dedupKey, summary string, _ pagerduty.Severity, _ map[string]any) error {
	f.triggerCalls++
	f.lastDedup = dedupKey
	f.lastSummary = summary
	return f.triggerErr
}

func (f *fakeAlertClient) Resolve(_ context.Context, dedupKey string) error {
	f.resolveCalls++
	f.lastDedup = dedupKey
	return f.resolveErr
}

func newWatcherWithMock(t *testing.T, pd worker.AlertClient) (*worker.DeadLetterWatcher, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := worker.DeadLetterWatcherConfig{
		Threshold:    5,
		Window:       time.Minute,
		PollInterval: time.Minute,
	}
	w := worker.NewDeadLetterWatcher(db, pd, cfg)
	return w, mock
}

func expectCount(mock sqlmock.Sqlmock, n int) {
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM dead_letter_events`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(n))
}

// --- trigger tests ---

func TestWatcher_TriggersWhenAboveThreshold(t *testing.T) {
	pd := &fakeAlertClient{}
	w, mock := newWatcherWithMock(t, pd)
	expectCount(mock, 10) // above threshold of 5

	w.Run(context.Background())

	if pd.triggerCalls != 1 {
		t.Errorf("expected 1 trigger call, got %d", pd.triggerCalls)
	}
	if pd.resolveCalls != 0 {
		t.Errorf("expected 0 resolve calls, got %d", pd.resolveCalls)
	}
}

func TestWatcher_DedupKeyIsStable(t *testing.T) {
	pd := &fakeAlertClient{}
	w, mock := newWatcherWithMock(t, pd)
	expectCount(mock, 10)

	w.Run(context.Background())

	if pd.lastDedup == "" {
		t.Error("expected non-empty dedup key")
	}
	dedup1 := pd.lastDedup

	// Reset watcher firing state to force another trigger
	w2, mock2 := newWatcherWithMock(t, pd)
	expectCount(mock2, 10)
	w2.Run(context.Background())

	if pd.lastDedup != dedup1 {
		t.Errorf("dedup key changed across restarts: %q != %q", pd.lastDedup, dedup1)
	}
}

func TestWatcher_NoTriggerWhenBelowThreshold(t *testing.T) {
	pd := &fakeAlertClient{}
	w, mock := newWatcherWithMock(t, pd)
	expectCount(mock, 3) // below threshold of 5

	w.Run(context.Background())

	if pd.triggerCalls != 0 {
		t.Errorf("expected 0 trigger calls, got %d", pd.triggerCalls)
	}
}

func TestWatcher_ResolvesWhenDropsBelowThreshold(t *testing.T) {
	pd := &fakeAlertClient{}
	w, mock := newWatcherWithMock(t, pd)

	// First run: above threshold → trigger
	expectCount(mock, 10)
	w.Run(context.Background())

	// Second run: below threshold → resolve
	expectCount(mock, 2)
	w.Run(context.Background())

	if pd.triggerCalls != 1 {
		t.Errorf("expected 1 trigger, got %d", pd.triggerCalls)
	}
	if pd.resolveCalls != 1 {
		t.Errorf("expected 1 resolve, got %d", pd.resolveCalls)
	}
}

func TestWatcher_NoDoubleTriggering(t *testing.T) {
	pd := &fakeAlertClient{}
	w, mock := newWatcherWithMock(t, pd)

	// Two consecutive runs above threshold — should only trigger once.
	expectCount(mock, 10)
	w.Run(context.Background())
	expectCount(mock, 12)
	w.Run(context.Background())

	if pd.triggerCalls != 1 {
		t.Errorf("expected 1 trigger (dedup), got %d", pd.triggerCalls)
	}
}

func TestWatcher_NoDoubleResolving(t *testing.T) {
	pd := &fakeAlertClient{}
	w, mock := newWatcherWithMock(t, pd)

	expectCount(mock, 10)
	w.Run(context.Background())
	expectCount(mock, 1)
	w.Run(context.Background())
	expectCount(mock, 2)
	w.Run(context.Background()) // still below, should not resolve again

	if pd.resolveCalls != 1 {
		t.Errorf("expected 1 resolve, got %d", pd.resolveCalls)
	}
}

func TestWatcher_NilPagerDutyClientSkipsAlert(t *testing.T) {
	w, mock := newWatcherWithMock(t, nil) // no PD client
	expectCount(mock, 10)

	// Should not panic
	w.Run(context.Background())
}

func TestWatcher_DBErrorLogsAndContinues(t *testing.T) {
	pd := &fakeAlertClient{}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM dead_letter_events`).
		WillReturnError(errors.New("db error"))

	cfg := worker.DefaultDeadLetterWatcherConfig()
	w := worker.NewDeadLetterWatcher(db, pd, cfg)
	w.Run(context.Background()) // should not panic

	if pd.triggerCalls != 0 {
		t.Errorf("expected 0 trigger calls on DB error, got %d", pd.triggerCalls)
	}
}

func TestWatcher_TriggerErrorLogged(t *testing.T) {
	pd := &fakeAlertClient{triggerErr: errors.New("pd down")}
	w, mock := newWatcherWithMock(t, pd)
	expectCount(mock, 10)

	// Should not panic even when trigger fails
	w.Run(context.Background())

	if pd.triggerCalls != 1 {
		t.Errorf("expected 1 trigger call, got %d", pd.triggerCalls)
	}
}

func TestWatcher_ResolveErrorLogged(t *testing.T) {
	pd := &fakeAlertClient{resolveErr: errors.New("pd down")}
	w, mock := newWatcherWithMock(t, pd)

	expectCount(mock, 10)
	w.Run(context.Background())
	expectCount(mock, 1)
	w.Run(context.Background()) // resolve will fail but should not panic

	if pd.resolveCalls != 1 {
		t.Errorf("expected 1 resolve call, got %d", pd.resolveCalls)
	}
}

func TestWatcher_ExactlyAtThresholdTriggers(t *testing.T) {
	pd := &fakeAlertClient{}
	w, mock := newWatcherWithMock(t, pd)
	expectCount(mock, 5) // exactly at threshold

	w.Run(context.Background())

	if pd.triggerCalls != 1 {
		t.Errorf("expected trigger at threshold, got %d triggers", pd.triggerCalls)
	}
}

func TestWatcher_StartStopsOnContextCancel(t *testing.T) {
	pd := &fakeAlertClient{}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// Expect at least the initial Run call
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM dead_letter_events`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	cfg := worker.DeadLetterWatcherConfig{
		Threshold:    5,
		Window:       time.Minute,
		PollInterval: 50 * time.Millisecond, // short for test speed
	}
	w := worker.NewDeadLetterWatcher(db, pd, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// expected
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop after context cancellation")
	}
}

func TestDefaultDeadLetterWatcherConfig(t *testing.T) {
	cfg := worker.DefaultDeadLetterWatcherConfig()
	if cfg.Threshold <= 0 {
		t.Error("expected positive threshold")
	}
	if cfg.Window <= 0 {
		t.Error("expected positive window")
	}
	if cfg.PollInterval <= 0 {
		t.Error("expected positive poll interval")
	}
}

// Ensure the *sql.DB constructor path in NewDeadLetterWatcher works correctly.
func TestNewDeadLetterWatcher_NonNilDB(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	w := worker.NewDeadLetterWatcher(db, nil, worker.DefaultDeadLetterWatcherConfig())
	if w == nil {
		t.Fatal("expected non-nil watcher")
	}
}

// --- compile-time interface check ---
var _ worker.AlertClient = (*fakeAlertClient)(nil)
var _ worker.AlertClient = (*pagerduty.Client)(nil)

// Ensure pagerduty.Client satisfies the AlertClient interface.
func init() {
	// Intentionally empty — the var _ declarations above are the check.
	_ = (*sql.DB)(nil)
}
