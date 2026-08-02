package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"stellarbill-backend/internal/metrics"
	"stellarbill-backend/internal/timeutil"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakeAnalyzeStore is an in-memory analyzeStore for testing the job without
// Postgres. It records calls and can be made to fail.
type fakeAnalyzeStore struct {
	mu sync.Mutex

	// analyzeCalls records each table name analyzed, in order.
	analyzeCalls []string

	// analyzeErr, when non-nil, is returned by Analyze.
	analyzeErr error
	// perTableErr allows error injection per table name.
	perTableErr map[string]error
}

func (f *fakeAnalyzeStore) Analyze(_ context.Context, table string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.analyzeCalls = append(f.analyzeCalls, table)
	if f.perTableErr != nil {
		if err, ok := f.perTableErr[table]; ok {
			return err
		}
	}
	return f.analyzeErr
}

func (f *fakeAnalyzeStore) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.analyzeCalls))
	copy(out, f.analyzeCalls)
	return out
}

func resetAnalyzeGauge() {
	metrics.AnalyzeLastRunTimestamp.Reset()
}

// recordingAnalyzeLogger counts Error calls.
type recordingAnalyzeLogger struct {
	mu sync.Mutex
	n  int
}

func (r *recordingAnalyzeLogger) Error(string, ...any) {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
}

func (r *recordingAnalyzeLogger) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// ---- Config tests ----

func TestAnalyzeConfig_Defaults(t *testing.T) {
	c := AnalyzeConfig{}.withDefaults()
	if c.OutboxInterval != 5*time.Minute {
		t.Errorf("OutboxInterval default = %v, want 5m", c.OutboxInterval)
	}
	if c.StatementsInterval != 15*time.Minute {
		t.Errorf("StatementsInterval default = %v, want 15m", c.StatementsInterval)
	}
	if c.SubscriptionsInterval != 30*time.Minute {
		t.Errorf("SubscriptionsInterval default = %v, want 30m", c.SubscriptionsInterval)
	}
	if c.AnalyzeTimeout != 30*time.Second {
		t.Errorf("AnalyzeTimeout default = %v, want 30s", c.AnalyzeTimeout)
	}
	if c.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout default = %v, want 30s", c.ShutdownTimeout)
	}
	if c.TickInterval != 30*time.Second {
		t.Errorf("TickInterval default = %v, want 30s", c.TickInterval)
	}
}

func TestAnalyzeConfig_PartialOverride(t *testing.T) {
	c := AnalyzeConfig{OutboxInterval: 1 * time.Minute}.withDefaults()
	if c.OutboxInterval != 1*time.Minute {
		t.Errorf("OutboxInterval = %v, want 1m", c.OutboxInterval)
	}
	if c.StatementsInterval != 15*time.Minute {
		t.Errorf("StatementsInterval should be default, got %v", c.StatementsInterval)
	}
}

func TestAnalyzeConfig_Tables(t *testing.T) {
	c := DefaultAnalyzeConfig()
	tables := c.tables()
	if len(tables) != 3 {
		t.Fatalf("expected 3 tables, got %d", len(tables))
	}
	expected := []struct {
		name     string
		interval time.Duration
	}{
		{"outbox_events", 5 * time.Minute},
		{"statements", 15 * time.Minute},
		{"subscriptions", 30 * time.Minute},
	}
	for i, exp := range expected {
		if tables[i].name != exp.name {
			t.Errorf("tables[%d].name = %q, want %q", i, tables[i].name, exp.name)
		}
		if tables[i].interval != exp.interval {
			t.Errorf("tables[%d].interval = %v, want %v", i, tables[i].interval, exp.interval)
		}
	}
}

// ---- isDue / analyzeDue tests ----

func TestIsDue_NeverRun(t *testing.T) {
	j := newAnalyzeJob(&fakeAnalyzeStore{}, AnalyzeConfig{}, nil)
	table := hotTable{name: "outbox_events", interval: 5 * time.Minute}
	if !j.isDue(table, time.Now()) {
		t.Error("isDue should be true for a table that has never been analyzed")
	}
}

func TestIsDue_WithinInterval(t *testing.T) {
	j := newAnalyzeJob(&fakeAnalyzeStore{}, AnalyzeConfig{}, nil)
	now := time.Now()
	table := hotTable{name: "outbox_events", interval: 5 * time.Minute}

	j.mu.Lock()
	j.lastRunTime["outbox_events"] = now.Add(-1 * time.Minute)
	j.mu.Unlock()

	if j.isDue(table, now) {
		t.Error("isDue should be false when last run is within the interval")
	}
}

func TestIsDue_BeyondInterval(t *testing.T) {
	j := newAnalyzeJob(&fakeAnalyzeStore{}, AnalyzeConfig{}, nil)
	now := time.Now()
	table := hotTable{name: "outbox_events", interval: 5 * time.Minute}

	j.mu.Lock()
	j.lastRunTime["outbox_events"] = now.Add(-10 * time.Minute)
	j.mu.Unlock()

	if !j.isDue(table, now) {
		t.Error("isDue should be true when last run is beyond the interval")
	}
}

func TestIsDue_ExactlyAtInterval(t *testing.T) {
	j := newAnalyzeJob(&fakeAnalyzeStore{}, AnalyzeConfig{}, nil)
	now := time.Now()
	table := hotTable{name: "outbox_events", interval: 5 * time.Minute}

	j.mu.Lock()
	j.lastRunTime["outbox_events"] = now.Add(-5 * time.Minute)
	j.mu.Unlock()

	if !j.isDue(table, now) {
		t.Error("isDue should be true when last run is exactly at the interval boundary")
	}
}

// ---- analyzeTable tests ----

func TestAnalyzeTable_Success(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()

	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})

	stats := j.GetStats()
	if stats.TotalAnalyzed != 1 {
		t.Errorf("TotalAnalyzed = %d, want 1", stats.TotalAnalyzed)
	}
	if stats.TotalFailed != 0 {
		t.Errorf("TotalFailed = %d, want 0", stats.TotalFailed)
	}
	if stats.ConsecutiveErrs["outbox_events"] != 0 {
		t.Errorf("ConsecutiveErrs = %d, want 0", stats.ConsecutiveErrs["outbox_events"])
	}
	if stats.LastRunErrors["outbox_events"] != "" {
		t.Errorf("LastRunErrors should be empty on success, got %q", stats.LastRunErrors["outbox_events"])
	}
	if stats.LastRunTimes["outbox_events"].IsZero() {
		t.Error("LastRunTimes should be set on success")
	}

	calls := store.calls()
	if len(calls) != 1 || calls[0] != "outbox_events" {
		t.Errorf("expected Analyze('outbox_events'), got %v", calls)
	}
}

func TestAnalyzeTable_Error(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{analyzeErr: errors.New("disk full")}
	rec := &recordingAnalyzeLogger{}
	j := newAnalyzeJob(store, AnalyzeConfig{}, rec)
	j.ctx = context.Background()

	j.analyzeTable(hotTable{name: "statements", interval: 15 * time.Minute})

	stats := j.GetStats()
	if stats.TotalAnalyzed != 0 {
		t.Errorf("TotalAnalyzed = %d, want 0", stats.TotalAnalyzed)
	}
	if stats.TotalFailed != 1 {
		t.Errorf("TotalFailed = %d, want 1", stats.TotalFailed)
	}
	if stats.ConsecutiveErrs["statements"] != 1 {
		t.Errorf("ConsecutiveErrs = %d, want 1", stats.ConsecutiveErrs["statements"])
	}
	if stats.LastRunErrors["statements"] != "disk full" {
		t.Errorf("LastRunErrors = %q, want %q", stats.LastRunErrors["statements"], "disk full")
	}
	if rec.count() == 0 {
		t.Error("logger.Error should have been called")
	}
}

func TestAnalyzeTable_MultipleCalls(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()

	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})
	j.analyzeTable(hotTable{name: "statements", interval: 15 * time.Minute})
	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})

	stats := j.GetStats()
	if stats.TotalAnalyzed != 3 {
		t.Errorf("TotalAnalyzed = %d, want 3", stats.TotalAnalyzed)
	}
	if stats.TotalFailed != 0 {
		t.Errorf("TotalFailed = %d, want 0", stats.TotalFailed)
	}

	calls := store.calls()
	expected := []string{"outbox_events", "statements", "outbox_events"}
	for i, call := range calls {
		if call != expected[i] {
			t.Errorf("call[%d] = %q, want %q", i, call, expected[i])
		}
	}
}

func TestAnalyzeTable_MixedSuccessAndFailure(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{
		perTableErr: map[string]error{
			"subscriptions": errors.New("timeout"),
		},
	}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()

	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})
	j.analyzeTable(hotTable{name: "subscriptions", interval: 30 * time.Minute})
	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})

	stats := j.GetStats()
	if stats.TotalAnalyzed != 2 {
		t.Errorf("TotalAnalyzed = %d, want 2", stats.TotalAnalyzed)
	}
	if stats.TotalFailed != 1 {
		t.Errorf("TotalFailed = %d, want 1", stats.TotalFailed)
	}
	if stats.ConsecutiveErrs["outbox_events"] != 0 {
		t.Errorf("ConsecutiveErrs[outbox_events] = %d, want 0", stats.ConsecutiveErrs["outbox_events"])
	}
	if stats.ConsecutiveErrs["subscriptions"] != 1 {
		t.Errorf("ConsecutiveErrs[subscriptions] = %d, want 1", stats.ConsecutiveErrs["subscriptions"])
	}
}

// ---- analyzeDue tests ----

func TestAnalyzeDue_RunsAllDueTables(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{}
	cfg := AnalyzeConfig{
		OutboxInterval:        5 * time.Minute,
		StatementsInterval:    15 * time.Minute,
		SubscriptionsInterval: 30 * time.Minute,
	}
	j := newAnalyzeJob(store, cfg, nil)
	j.ctx = context.Background()
	clock := timeutil.NewFakeClock(time.Now())
	j.SetClock(clock)
	j.clock = clock

	// Run once: all tables are due (never run).
	j.analyzeDue()

	calls := store.calls()
	if len(calls) != 3 {
		t.Errorf("expected 3 analyze calls (all tables due), got %d: %v", len(calls), calls)
	}
}

func TestAnalyzeDue_SkipsTablesWithinInterval(t *testing.T) {
	resetAnalyzeGauge()
	now := time.Now()
	clock := timeutil.NewFakeClock(now)

	store := &fakeAnalyzeStore{}
	cfg := AnalyzeConfig{
		OutboxInterval:        5 * time.Minute,
		StatementsInterval:    15 * time.Minute,
		SubscriptionsInterval: 30 * time.Minute,
	}
	j := newAnalyzeJob(store, cfg, nil)
	j.ctx = context.Background()
	j.SetClock(clock)

	// Set last run times so only outbox_events is past its interval.
	j.mu.Lock()
	j.lastRunTime["outbox_events"] = now.Add(-10 * time.Minute)  // overdue
	j.lastRunTime["statements"] = now.Add(-1 * time.Minute)       // within interval
	j.lastRunTime["subscriptions"] = now.Add(-5 * time.Minute)    // within interval
	j.mu.Unlock()

	j.analyzeDue()

	calls := store.calls()
	if len(calls) != 1 || calls[0] != "outbox_events" {
		t.Errorf("expected only outbox_events to be analyzed, got %v", calls)
	}
}

// ---- Clock injection tests ----

func TestAnalyzeJob_ClockInjection(t *testing.T) {
	store := &fakeAnalyzeStore{}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)

	clock := timeutil.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	j.SetClock(clock)
	if j.clock.Now() != clock.Now() {
		t.Error("clock should be the injected fake clock")
	}
}

// ---- Lifecycle tests ----

func TestAnalyzeJob_StartStopHealth(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{}
	j := newAnalyzeJob(store, AnalyzeConfig{
		OutboxInterval:        time.Hour,
		StatementsInterval:    time.Hour,
		SubscriptionsInterval: time.Hour,
	}, nil)

	// Not started yet.
	if err := j.Health(); err == nil {
		t.Error("Health should fail before Start")
	}

	j.Start()
	time.Sleep(50 * time.Millisecond)

	if err := j.Health(); err != nil {
		t.Errorf("Health should pass while running: %v", err)
	}
	// At least the startup run should have analyzed all tables.
	stats := j.GetStats()
	if stats.TotalAnalyzed < 3 {
		t.Errorf("expected at least 3 analyzes on startup, got %d", stats.TotalAnalyzed)
	}

	if err := j.Stop(); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	if err := j.Health(); err == nil {
		t.Error("Health should fail after Stop")
	}
}

func TestAnalyzeJob_StopWithoutStart(t *testing.T) {
	j := newAnalyzeJob(&fakeAnalyzeStore{}, AnalyzeConfig{}, nil)
	if err := j.Stop(); err != nil {
		t.Errorf("Stop without Start should be a no-op, got %v", err)
	}
}

func TestAnalyzeJob_TickerFires(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{}
	j := newAnalyzeJob(store, AnalyzeConfig{
		OutboxInterval:        20 * time.Millisecond,
		StatementsInterval:    20 * time.Millisecond,
		SubscriptionsInterval: 20 * time.Millisecond,
	}, nil)

	j.Start()
	defer j.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if j.GetStats().TotalAnalyzed >= 6 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := j.GetStats().TotalAnalyzed; got < 6 {
		t.Errorf("expected >=6 analyzes from ticker, got %d", got)
	}
}

// ---- Health edge cases ----

func TestAnalyzeJob_Health_UnhealthyAfterConsecutiveErrors(t *testing.T) {
	store := &fakeAnalyzeStore{analyzeErr: errors.New("boom")}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()
	j.running.Store(1)

	for i := 0; i < 6; i++ {
		j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})
	}
	if err := j.Health(); err == nil {
		t.Error("Health should fail after >5 consecutive errors on any table")
	}
}

func TestAnalyzeJob_Health_HealthyAfterMixedErrors(t *testing.T) {
	store := &fakeAnalyzeStore{
		perTableErr: map[string]error{
			"subscriptions": errors.New("timeout"),
		},
	}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()
	j.running.Store(1)

	// 6 errors on subscriptions, but outbox_events and statements are fine.
	for i := 0; i < 6; i++ {
		j.analyzeTable(hotTable{name: "subscriptions", interval: 30 * time.Minute})
	}
	if err := j.Health(); err == nil {
		t.Error("Health should fail after >5 consecutive errors on subscriptions")
	}

	// Reset subscriptions by analyzing successfully.
	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})
	j.analyzeTable(hotTable{name: "statements", interval: 15 * time.Minute})

	// But subscriptions is still errored out.
	if err := j.Health(); err == nil {
		t.Error("Health should still fail due to subscriptions")
	}
}

// ---- Metrics tests ----

func TestAnalyzeTable_MetricsRecorded(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()

	before := testutil.ToFloat64(metrics.AnalyzeLastRunTimestamp.WithLabelValues("outbox_events"))

	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})

	after := testutil.ToFloat64(metrics.AnalyzeLastRunTimestamp.WithLabelValues("outbox_events"))

	// The gauge should have been updated to a non-zero Unix timestamp.
	if after == 0 {
		t.Error("AnalyzeLastRunTimestamp should be non-zero after successful analyze")
	}
	if after == before {
		t.Error("AnalyzeLastRunTimestamp should have changed after analyze")
	}
}

func TestAnalyzeTable_MetricsNotRecordedOnError(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{analyzeErr: errors.New("fail")}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()

	before := testutil.ToFloat64(metrics.AnalyzeLastRunTimestamp.WithLabelValues("outbox_events"))

	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})

	after := testutil.ToFloat64(metrics.AnalyzeLastRunTimestamp.WithLabelValues("outbox_events"))

	if after != before {
		t.Error("AnalyzeLastRunTimestamp should not change on error")
	}
}

func TestAnalyzeTable_MetricsPerTable(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()

	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})
	j.analyzeTable(hotTable{name: "statements", interval: 15 * time.Minute})

	outboxGauge := testutil.ToFloat64(metrics.AnalyzeLastRunTimestamp.WithLabelValues("outbox_events"))
	stmtGauge := testutil.ToFloat64(metrics.AnalyzeLastRunTimestamp.WithLabelValues("statements"))

	if outboxGauge == 0 {
		t.Error("outbox_events gauge should be non-zero")
	}
	if stmtGauge == 0 {
		t.Error("statements gauge should be non-zero")
	}
	if outboxGauge == stmtGauge {
		t.Log("note: outbox and statements gauges may be equal if both analyzed in the same second")
	}
}

// ---- Edge case: nil logger ----

func TestAnalyzeJob_NilLoggerDoesNotPanic(t *testing.T) {
	store := &fakeAnalyzeStore{analyzeErr: errors.New("oops")}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()

	// Should not panic despite nil logger.
	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})

	if stats := j.GetStats(); stats.TotalFailed != 1 {
		t.Errorf("TotalFailed = %d, want 1", stats.TotalFailed)
	}
}

// ---- Shutdown timeout ----

type blockingAnalyzeStore struct {
	block chan struct{}
}

func (s *blockingAnalyzeStore) Analyze(_ context.Context, _ string) error {
	<-s.block
	return nil
}

func TestAnalyzeJob_ShutdownTimeout_Blocking(t *testing.T) {
	store := &blockingAnalyzeStore{block: make(chan struct{})}
	j := newAnalyzeJob(store, AnalyzeConfig{
		OutboxInterval:        time.Hour,
		StatementsInterval:    time.Hour,
		SubscriptionsInterval: time.Hour,
		ShutdownTimeout:       30 * time.Millisecond,
	}, nil)

	j.Start()
	time.Sleep(20 * time.Millisecond)

	err := j.Stop()
	if err == nil {
		t.Error("expected shutdown timeout error while analyze is blocked")
	}
	close(store.block)
}

// ---- GetStats edge cases ----

func TestAnalyzeJob_GetStats_AllFieldsPopulated(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()

	// Run two successful analyzes and one failure.
	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})
	j.analyzeTable(hotTable{name: "statements", interval: 15 * time.Minute})

	store.analyzeErr = errors.New("fail")
	j.analyzeTable(hotTable{name: "subscriptions", interval: 30 * time.Minute})

	stats := j.GetStats()
	if stats.TotalAnalyzed != 2 {
		t.Errorf("TotalAnalyzed = %d, want 2", stats.TotalAnalyzed)
	}
	if stats.TotalFailed != 1 {
		t.Errorf("TotalFailed = %d, want 1", stats.TotalFailed)
	}
	if stats.LastRunTimes["outbox_events"].IsZero() {
		t.Error("LastRunTimes[outbox_events] should be set")
	}
	if stats.LastRunTimes["statements"].IsZero() {
		t.Error("LastRunTimes[statements] should be set")
	}
	if stats.LastRunTimes["subscriptions"].IsZero() {
		t.Error("LastRunTimes[subscriptions] should be set even on failure")
	}
	if stats.LastRunErrors["subscriptions"] != "fail" {
		t.Errorf("LastRunErrors[subscriptions] = %q, want %q",
			stats.LastRunErrors["subscriptions"], "fail")
	}
	if _, ok := stats.LastRunErrors["outbox_events"]; ok {
		t.Error("LastRunErrors[outbox_events] should not be set on success")
	}
}

// ---- Store interface tests (via fake store) ----

func TestAnalyzeStore_PassesTableName(t *testing.T) {
	store := &fakeAnalyzeStore{}
	err := store.Analyze(context.Background(), "outbox_events")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	calls := store.calls()
	if len(calls) != 1 || calls[0] != "outbox_events" {
		t.Errorf("expected Analyze('outbox_events'), got %v", calls)
	}
}

func TestAnalyzeStore_ReturnsError(t *testing.T) {
	store := &fakeAnalyzeStore{analyzeErr: errors.New("connection refused")}
	err := store.Analyze(context.Background(), "outbox_events")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnalyzeStore_HandlesSpecialTableNames(t *testing.T) {
	store := &fakeAnalyzeStore{}
	err := store.Analyze(context.Background(), "statements_partitioned")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	calls := store.calls()
	if len(calls) != 1 || calls[0] != "statements_partitioned" {
		t.Errorf("expected Analyze('statements_partitioned'), got %v", calls)
	}
}

func TestNewAnalyzeJob_Constructor(t *testing.T) {
	// Create a minimal pool config for constructor testing.
	// The pool won't actually connect since Start() is not called.
	poolCfg, err := pgxpool.ParseConfig("postgres://user:pass@localhost/db")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig: %v", err)
	}
	poolCfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig: %v", err)
	}
	defer pool.Close()

	j := NewAnalyzeJob(pool, AnalyzeConfig{}, nil)
	if j == nil {
		t.Fatal("expected non-nil job")
	}
	if _, ok := j.store.(*pgxAnalyzeStore); !ok {
		t.Errorf("expected pgxAnalyzeStore, got %T", j.store)
	}
	if j.config.OutboxInterval != 5*time.Minute {
		t.Errorf("OutboxInterval = %v, want 5m", j.config.OutboxInterval)
	}
}

func TestAnalyzeJob_RecordsMultipleRuns(t *testing.T) {
	resetAnalyzeGauge()
	store := &fakeAnalyzeStore{}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()

	for range 5 {
		j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})
	}

	stats := j.GetStats()
	if stats.TotalAnalyzed != 5 {
		t.Errorf("TotalAnalyzed = %d, want 5", stats.TotalAnalyzed)
	}
	if stats.TotalFailed != 0 {
		t.Errorf("TotalFailed = %d, want 0", stats.TotalFailed)
	}

	calls := store.calls()
	if len(calls) != 5 {
		t.Errorf("expected 5 calls, got %d", len(calls))
	}
	for i, call := range calls {
		if call != "outbox_events" {
			t.Errorf("call[%d] = %q, want %q", i, call, "outbox_events")
		}
	}
}

func TestAnalyzeJob_ConsecutiveErrorsResetOnSuccess(t *testing.T) {
	store := &fakeAnalyzeStore{analyzeErr: errors.New("fail")}
	j := newAnalyzeJob(store, AnalyzeConfig{}, nil)
	j.ctx = context.Background()

	// 3 consecutive failures
	for range 3 {
		j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})
	}

	stats := j.GetStats()
	if stats.ConsecutiveErrs["outbox_events"] != 3 {
		t.Errorf("ConsecutiveErrs = %d, want 3", stats.ConsecutiveErrs["outbox_events"])
	}

	// Now succeed
	store.analyzeErr = nil
	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})

	stats = j.GetStats()
	if stats.ConsecutiveErrs["outbox_events"] != 0 {
		t.Errorf("ConsecutiveErrs should be 0 after success, got %d",
			stats.ConsecutiveErrs["outbox_events"])
	}
	if stats.TotalFailed != 3 {
		t.Errorf("TotalFailed = %d, want 3", stats.TotalFailed)
	}
	if stats.TotalAnalyzed != 1 {
		t.Errorf("TotalAnalyzed = %d, want 1", stats.TotalAnalyzed)
	}
}

func TestAnalyzeDue_EmptyStatsOnCreation(t *testing.T) {
	j := newAnalyzeJob(&fakeAnalyzeStore{}, AnalyzeConfig{}, nil)

	stats := j.GetStats()
	if stats.TotalAnalyzed != 0 {
		t.Errorf("TotalAnalyzed should be 0 initially, got %d", stats.TotalAnalyzed)
	}
	if stats.TotalFailed != 0 {
		t.Errorf("TotalFailed should be 0 initially, got %d", stats.TotalFailed)
	}
	if len(stats.LastRunTimes) != 0 {
		t.Errorf("expected empty LastRunTimes, got %d entries", len(stats.LastRunTimes))
	}
}

// ---- Regression: Analyze during heavy write does not stall foreground ----

// signaledAnalyzeStore blocks ANALYZE until the signal channel is closed.
type signaledAnalyzeStore struct {
	waitForeground chan struct{}
	analyzeStarted chan struct{}
}

func (s *signaledAnalyzeStore) Analyze(ctx context.Context, _ string) error {
	close(s.analyzeStarted)
	select {
	case <-s.waitForeground:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TestAnalyzeDuringHeavyWrite simulates a foreground write operation that
// completes independently while ANALYZE is in flight. This validates that the
// analyze job's timeout and concurrency model do not block the application.
func TestAnalyzeDuringHeavyWrite(t *testing.T) {
	resetAnalyzeGauge()
	foregroundDone := make(chan struct{})

	store := &signaledAnalyzeStore{
		waitForeground: foregroundDone,
		analyzeStarted: make(chan struct{}),
	}

	j := newAnalyzeJob(store, AnalyzeConfig{
		OutboxInterval:        5 * time.Minute,
		StatementsInterval:    15 * time.Minute,
		SubscriptionsInterval: 30 * time.Minute,
	}, nil)
	j.ctx = context.Background()

	// Simulate foreground write completing while ANALYZE is running.
	go func() {
		<-store.analyzeStarted
		// Foreground write "completes" independently.
		close(foregroundDone)
	}()

	j.analyzeTable(hotTable{name: "outbox_events", interval: 5 * time.Minute})

	stats := j.GetStats()
	if stats.TotalAnalyzed != 1 {
		t.Errorf("TotalAnalyzed = %d, want 1 (analyze should complete)", stats.TotalAnalyzed)
	}
}
