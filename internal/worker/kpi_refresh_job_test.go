package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"stellarbill-backend/internal/metrics"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakeKpiStore is an in-memory kpiStore for testing the refresh job without
// Postgres. Each method returns pre-configured values or errors.
type fakeKpiStore struct {
	mu sync.Mutex

	mrrCents         int64
	mrrErr           error
	planCounts       map[KpiPlanKey]int64
	planErr          error
	churnRate        float64
	churnErr         error

	computeMRRCalls           int
	countActiveCalls          int
	computeChurnCalls         int
}

func (f *fakeKpiStore) ComputeMRRInCents(_ context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.computeMRRCalls++
	return f.mrrCents, f.mrrErr
}

func (f *fakeKpiStore) CountActiveSubscribersPerPlan(_ context.Context) (map[KpiPlanKey]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.countActiveCalls++
	if f.planCounts == nil {
		return map[KpiPlanKey]int64{}, f.planErr
	}
	out := make(map[KpiPlanKey]int64, len(f.planCounts))
	for k, v := range f.planCounts {
		out[k] = v
	}
	return out, f.planErr
}

func (f *fakeKpiStore) ComputeChurnRate24h(_ context.Context) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.computeChurnCalls++
	return f.churnRate, f.churnErr
}

func resetKpiGauges() {
	metrics.MrrCents.Set(0)
	metrics.ActiveSubscribersTotal.Reset()
	metrics.ChurnRate24h.Set(0)
}

// recordingKpiLogger counts Error calls.
type recordingKpiLogger struct {
	mu sync.Mutex
	n  int
}

func (r *recordingKpiLogger) Error(string, ...any) {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
}

func (r *recordingKpiLogger) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func TestKpiRefreshConfig_Defaults(t *testing.T) {
	c := KpiRefreshConfig{}.withDefaults()
	if c.PollInterval != time.Hour {
		t.Errorf("PollInterval default = %v, want 1h", c.PollInterval)
	}
	if c.QueryTimeout != 30*time.Second {
		t.Errorf("QueryTimeout default = %v, want 30s", c.QueryTimeout)
	}
	if c.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout default = %v, want 30s", c.ShutdownTimeout)
	}
}

func TestKpiRefreshConfig_PartialOverride(t *testing.T) {
	c := KpiRefreshConfig{PollInterval: 10 * time.Minute}.withDefaults()
	if c.PollInterval != 10*time.Minute {
		t.Errorf("PollInterval = %v, want 10m", c.PollInterval)
	}
	if c.QueryTimeout != 30*time.Second {
		t.Errorf("QueryTimeout should be default, got %v", c.QueryTimeout)
	}
}

func TestKpiRefreshOnce_Success(t *testing.T) {
	resetKpiGauges()
	store := &fakeKpiStore{
		mrrCents:   1234500,
		planCounts: map[KpiPlanKey]int64{{ID: "plan_a", Name: "Plan A"}: 10, {ID: "plan_b", Name: "Plan B"}: 5},
		churnRate:  0.03,
	}
	j := newKpiRefreshJob(store, KpiRefreshConfig{}, nil)
	j.ctx = context.Background()

	j.refreshOnce()

	stats := j.GetStats()
	if stats.Refreshed != 1 {
		t.Errorf("Refreshed = %d, want 1", stats.Refreshed)
	}
	if stats.Failed != 0 {
		t.Errorf("Failed = %d, want 0", stats.Failed)
	}
	if stats.ConsecutiveErr != 0 {
		t.Errorf("ConsecutiveErr = %d, want 0", stats.ConsecutiveErr)
	}

	if got := testutil.ToFloat64(metrics.MrrCents); got != 1234500 {
		t.Errorf("MrrCents = %f, want 1234500", got)
	}
	if got := testutil.ToFloat64(metrics.ActiveSubscribersTotal.WithLabelValues("plan_a", "Plan A")); got != 10 {
		t.Errorf("active_subscribers_total{plan_a} = %f, want 10", got)
	}
	if got := testutil.ToFloat64(metrics.ActiveSubscribersTotal.WithLabelValues("plan_b", "Plan B")); got != 5 {
		t.Errorf("active_subscribers_total{plan_b} = %f, want 5", got)
	}
	if got := testutil.ToFloat64(metrics.ChurnRate24h); got != 0.03 {
		t.Errorf("ChurnRate24h = %f, want 0.03", got)
	}
}

func TestKpiRefreshOnce_EmptyPlans(t *testing.T) {
	resetKpiGauges()
	store := &fakeKpiStore{
		mrrCents:   0,
		planCounts: map[KpiPlanKey]int64{},
		churnRate:  0,
	}
	j := newKpiRefreshJob(store, KpiRefreshConfig{}, nil)
	j.ctx = context.Background()

	j.refreshOnce()

	stats := j.GetStats()
	if stats.Failed != 0 {
		t.Errorf("Failed = %d, want 0 (empty data should not error)", stats.Failed)
	}
}

func TestKpiRefreshOnce_PlanCountsNil(t *testing.T) {
	resetKpiGauges()
	store := &fakeKpiStore{
		mrrCents:  50000,
		planCounts: nil,
		churnRate:  0,
	}
	j := newKpiRefreshJob(store, KpiRefreshConfig{}, nil)
	j.ctx = context.Background()

	j.refreshOnce()

	if stats := j.GetStats(); stats.Failed != 0 {
		t.Errorf("Failed = %d, want 0 (nil plan counts should be treated as empty)", stats.Failed)
	}
}

func TestKpiRefreshOnce_ComputeMRRError(t *testing.T) {
	resetKpiGauges()
	store := &fakeKpiStore{mrrErr: errors.New("mrr query failed")}
	rec := &recordingKpiLogger{}
	j := newKpiRefreshJob(store, KpiRefreshConfig{}, rec)
	j.ctx = context.Background()

	j.refreshOnce()

	stats := j.GetStats()
	if stats.Failed != 1 {
		t.Errorf("Failed = %d, want 1", stats.Failed)
	}
	if stats.ConsecutiveErr != 1 {
		t.Errorf("ConsecutiveErr = %d, want 1", stats.ConsecutiveErr)
	}
	if rec.count() == 0 {
		t.Error("logger.Error should have been called")
	}
}

func TestKpiRefreshOnce_CountActiveError(t *testing.T) {
	resetKpiGauges()
	store := &fakeKpiStore{
		mrrCents: 1000,
		planErr:  errors.New("plan query failed"),
	}
	rec := &recordingKpiLogger{}
	j := newKpiRefreshJob(store, KpiRefreshConfig{}, rec)
	j.ctx = context.Background()

	j.refreshOnce()

	if stats := j.GetStats(); stats.Failed != 1 {
		t.Errorf("Failed = %d, want 1", stats.Failed)
	}
}

func TestKpiRefreshOnce_ChurnRateError(t *testing.T) {
	resetKpiGauges()
	store := &fakeKpiStore{
		mrrCents:   1000,
		planCounts: map[KpiPlanKey]int64{{ID: "p1", Name: "P1"}: 5},
		churnErr:   errors.New("churn query failed"),
	}
	rec := &recordingKpiLogger{}
	j := newKpiRefreshJob(store, KpiRefreshConfig{}, rec)
	j.ctx = context.Background()

	j.refreshOnce()

	if stats := j.GetStats(); stats.Failed != 1 {
		t.Errorf("Failed = %d, want 1", stats.Failed)
	}
}

func TestKpiRefreshJob_StartStopHealth(t *testing.T) {
	resetKpiGauges()
	store := &fakeKpiStore{
		mrrCents:   50000,
		planCounts: map[KpiPlanKey]int64{{ID: "basic", Name: "Basic"}: 3},
		churnRate:  0.01,
	}
	j := newKpiRefreshJob(store, KpiRefreshConfig{PollInterval: time.Hour}, nil)

	if err := j.Health(); err == nil {
		t.Error("Health should fail before Start")
	}

	j.Start()
	time.Sleep(50 * time.Millisecond)

	if err := j.Health(); err != nil {
		t.Errorf("Health should pass while running: %v", err)
	}
	if got := j.GetStats().Refreshed; got < 1 {
		t.Errorf("expected at least one startup refresh, got %d", got)
	}

	if err := j.Stop(); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
	if err := j.Health(); err == nil {
		t.Error("Health should fail after Stop")
	}
}

func TestKpiRefreshJob_StopWithoutStart(t *testing.T) {
	j := newKpiRefreshJob(&fakeKpiStore{}, KpiRefreshConfig{}, nil)
	if err := j.Stop(); err != nil {
		t.Errorf("Stop without Start should be a no-op, got %v", err)
	}
}

func TestKpiRefreshJob_TickerFires(t *testing.T) {
	resetKpiGauges()
	store := &fakeKpiStore{
		mrrCents:   100,
		planCounts: map[KpiPlanKey]int64{{ID: "x", Name: "X"}: 1},
		churnRate:  0,
	}
	j := newKpiRefreshJob(store, KpiRefreshConfig{PollInterval: 20 * time.Millisecond}, nil)

	j.Start()
	defer j.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if j.GetStats().Refreshed >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := j.GetStats().Refreshed; got < 2 {
		t.Errorf("expected >=2 refreshes from ticker, got %d", got)
	}
}

// blockingKpiStore blocks all methods until the block channel is closed.
type blockingKpiStore struct {
	block chan struct{}
}

func (s *blockingKpiStore) ComputeMRRInCents(_ context.Context) (int64, error) {
	<-s.block
	return 0, nil
}
func (s *blockingKpiStore) CountActiveSubscribersPerPlan(_ context.Context) (map[KpiPlanKey]int64, error) {
	<-s.block
	return map[KpiPlanKey]int64{}, nil
}
func (s *blockingKpiStore) ComputeChurnRate24h(_ context.Context) (float64, error) {
	<-s.block
	return 0, nil
}

func TestKpiRefreshJob_ShutdownTimeout_Blocking(t *testing.T) {
	store := &blockingKpiStore{block: make(chan struct{})}
	j := newKpiRefreshJob(store, KpiRefreshConfig{
		PollInterval:    time.Hour,
		ShutdownTimeout: 30 * time.Millisecond,
	}, nil)

	j.Start()
	time.Sleep(20 * time.Millisecond)

	err := j.Stop()
	if err == nil {
		t.Error("expected shutdown timeout error while refresh is blocked")
	}
	close(store.block)
}

func TestKpiRefreshJob_HealthUnhealthyAfterConsecutiveErrors(t *testing.T) {
	store := &fakeKpiStore{mrrErr: errors.New("boom")}
	j := newKpiRefreshJob(store, KpiRefreshConfig{}, nil)
	j.ctx = context.Background()
	j.running.Store(1)

	for i := 0; i < 6; i++ {
		j.refreshOnce()
	}
	if err := j.Health(); err == nil {
		t.Error("Health should fail after >5 consecutive errors")
	}
}

func TestKpiRefreshJob_RecordsMultipleRefreshCalls(t *testing.T) {
	resetKpiGauges()
	store := &fakeKpiStore{
		mrrCents:   100,
		planCounts: map[KpiPlanKey]int64{{ID: "p", Name: "P"}: 2},
		churnRate:  0.5,
	}
	j := newKpiRefreshJob(store, KpiRefreshConfig{}, nil)
	j.ctx = context.Background()

	j.refreshOnce()
	j.refreshOnce()
	j.refreshOnce()

	stats := j.GetStats()
	if stats.Refreshed != 3 {
		t.Errorf("Refreshed = %d, want 3", stats.Refreshed)
	}
	if stats.Failed != 0 {
		t.Errorf("Failed = %d, want 0", stats.Failed)
	}
}

func TestKpiRefreshJob_GetStatsLastRunError(t *testing.T) {
	store := &fakeKpiStore{mrrErr: errors.New("disk full")}
	j := newKpiRefreshJob(store, KpiRefreshConfig{}, nil)
	j.ctx = context.Background()

	j.refreshOnce()

	stats := j.GetStats()
	if stats.LastRunError != "compute MRR: disk full" {
		t.Errorf("LastRunError = %q, want %q", stats.LastRunError, "compute MRR: disk full")
	}
}

func TestKpiRefreshJob_NilLoggerDoesNotPanic(t *testing.T) {
	store := &fakeKpiStore{mrrErr: errors.New("oops")}
	j := newKpiRefreshJob(store, KpiRefreshConfig{}, nil)
	j.ctx = context.Background()

	// Should not panic despite nil logger.
	j.refreshOnce()

	if stats := j.GetStats(); stats.Failed != 1 {
		t.Errorf("Failed = %d, want 1", stats.Failed)
	}
}

// ---- SQL store tests with go-sqlmock ----

func newKpiSQLMock(t *testing.T) (*sqlKpiStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	return &sqlKpiStore{db: db}, mock, func() { _ = db.Close() }
}

func TestSQLKpiStore_ComputeMRRInCents_Success(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(amount \\* 100\\), 0\\)::bigint FROM subscriptions WHERE status = 'active'").
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(50000)))

	cents, err := store.ComputeMRRInCents(context.Background())
	if err != nil {
		t.Fatalf("ComputeMRRInCents: %v", err)
	}
	if cents != 50000 {
		t.Errorf("got %d, want 50000", cents)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSQLKpiStore_ComputeMRRInCents_Empty(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(amount \\* 100\\), 0\\)::bigint").
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(nil))

	cents, err := store.ComputeMRRInCents(context.Background())
	if err != nil {
		t.Fatalf("ComputeMRRInCents: %v", err)
	}
	if cents != 0 {
		t.Errorf("got %d, want 0 for empty result", cents)
	}
}

func TestSQLKpiStore_ComputeMRRInCents_Error(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	mock.ExpectQuery("SELECT COALESCE").WillReturnError(errors.New("connection refused"))

	if _, err := store.ComputeMRRInCents(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestSQLKpiStore_CountActiveSubscribersPerPlan_Success(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	mock.ExpectQuery(`SELECT plan_id, COALESCE\(plan_name, plan_id\), COUNT\(\*\)::bigint FROM subscriptions WHERE status = 'active' GROUP BY plan_id, plan_name ORDER BY plan_id`).
		WillReturnRows(sqlmock.NewRows([]string{"plan_id", "plan_name", "count"}).
			AddRow("plan_a", "Plan A", int64(10)).
			AddRow("plan_b", "Plan B", int64(5)))

	counts, err := store.CountActiveSubscribersPerPlan(context.Background())
	if err != nil {
		t.Fatalf("CountActiveSubscribersPerPlan: %v", err)
	}
	if len(counts) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(counts))
	}
	if counts[KpiPlanKey{ID: "plan_a", Name: "Plan A"}] != 10 {
		t.Errorf("plan_a count = %d, want 10", counts[KpiPlanKey{ID: "plan_a", Name: "Plan A"}])
	}
	if counts[KpiPlanKey{ID: "plan_b", Name: "Plan B"}] != 5 {
		t.Errorf("plan_b count = %d, want 5", counts[KpiPlanKey{ID: "plan_b", Name: "Plan B"}])
	}
}

func TestSQLKpiStore_CountActiveSubscribersPerPlan_Empty(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	mock.ExpectQuery("SELECT plan_id, COALESCE").
		WillReturnRows(sqlmock.NewRows([]string{"plan_id", "plan_name", "count"}))

	counts, err := store.CountActiveSubscribersPerPlan(context.Background())
	if err != nil {
		t.Fatalf("CountActiveSubscribersPerPlan: %v", err)
	}
	if len(counts) != 0 {
		t.Errorf("expected empty map, got %d entries", len(counts))
	}
}

func TestSQLKpiStore_CountActiveSubscribersPerPlan_Error(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	mock.ExpectQuery("SELECT plan_id, COALESCE").WillReturnError(errors.New("timeout"))

	if _, err := store.CountActiveSubscribersPerPlan(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestSQLKpiStore_CountActiveSubscribersPerPlan_ScanError(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	// Return a row with an incompatible type to trigger a scan error.
	mock.ExpectQuery("SELECT plan_id, COALESCE").
		WillReturnRows(sqlmock.NewRows([]string{"plan_id", "plan_name", "count"}).
			AddRow("p1", "P1", "not_a_number"))

	if _, err := store.CountActiveSubscribersPerPlan(context.Background()); err == nil {
		t.Fatal("expected scan error")
	}
}

func TestSQLKpiStore_CountActiveSubscribersPerPlan_RowsError(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	mock.ExpectQuery("SELECT plan_id, COALESCE").
		WillReturnRows(sqlmock.NewRows([]string{"plan_id", "plan_name", "count"}).
			AddRow("p1", "P1", int64(1)).
			RowError(0, errors.New("row iteration failed")))

	if _, err := store.CountActiveSubscribersPerPlan(context.Background()); err == nil {
		t.Fatal("expected row iteration error")
	}
}

func TestSQLKpiStore_ComputeChurnRate24h_Success(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	mock.ExpectQuery(`COUNT\(\*\) FILTER`).
		WillReturnRows(sqlmock.NewRows([]string{"float8"}).AddRow(0.05))

	rate, err := store.ComputeChurnRate24h(context.Background())
	if err != nil {
		t.Fatalf("ComputeChurnRate24h: %v", err)
	}
	if rate != 0.05 {
		t.Errorf("got %f, want 0.05", rate)
	}
}

func TestSQLKpiStore_ComputeChurnRate24h_NoChurn(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	mock.ExpectQuery("COUNT\\(\\*\\) FILTER").
		WillReturnRows(sqlmock.NewRows([]string{"float8"}).AddRow(0.0))

	rate, err := store.ComputeChurnRate24h(context.Background())
	if err != nil {
		t.Fatalf("ComputeChurnRate24h: %v", err)
	}
	if rate != 0.0 {
		t.Errorf("got %f, want 0.0", rate)
	}
}

func TestSQLKpiStore_ComputeChurnRate24h_NullResult(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	// NULLIF causes a null result when there are no matching rows.
	mock.ExpectQuery("COUNT\\(\\*\\) FILTER").
		WillReturnRows(sqlmock.NewRows([]string{"float8"}).AddRow(nil))

	rate, err := store.ComputeChurnRate24h(context.Background())
	if err != nil {
		t.Fatalf("ComputeChurnRate24h: %v", err)
	}
	if rate != 0.0 {
		t.Errorf("got %f, want 0.0 for null (empty base)", rate)
	}
}

func TestSQLKpiStore_ComputeChurnRate24h_Error(t *testing.T) {
	store, mock, done := newKpiSQLMock(t)
	defer done()

	mock.ExpectQuery("COUNT\\(\\*\\) FILTER").WillReturnError(errors.New("db down"))

	if _, err := store.ComputeChurnRate24h(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewKpiRefreshJob_Constructor(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	j := NewKpiRefreshJob(db, KpiRefreshConfig{}, nil)
	if j == nil {
		t.Fatal("expected non-nil job")
	}
	if _, ok := j.store.(*sqlKpiStore); !ok {
		t.Errorf("expected sqlKpiStore, got %T", j.store)
	}
	if j.config.PollInterval != time.Hour {
		t.Errorf("PollInterval = %v, want 1h", j.config.PollInterval)
	}
}