package worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeFeeRevenueStore is an in-memory feeRevenueStore for testing the refresh
// job without Postgres. It records how each Refresh was invoked and can be made
// to fail or to simulate a not-yet-populated view.
type fakeFeeRevenueStore struct {
	mu sync.Mutex

	// refreshCalls records the `concurrently` flag of each Refresh call, in order.
	refreshCalls []bool

	// refreshErr, when non-nil, is returned by Refresh.
	refreshErr error
	// notPopulatedOnConcurrent makes the first CONCURRENTLY refresh fail with a
	// "has not been populated" error (cleared after firing once).
	notPopulatedOnConcurrent bool

	// markErr, when non-nil, is returned by MarkRefreshed.
	markErr error

	lastRefreshed time.Time
	refreshedSet  bool

	// refreshHook, if set, runs inside Refresh (used to simulate a concurrent
	// long-running reader observing the refresh).
	refreshHook func(concurrently bool)
}

func (f *fakeFeeRevenueStore) Refresh(_ context.Context, concurrently bool) error {
	f.mu.Lock()
	f.refreshCalls = append(f.refreshCalls, concurrently)
	hook := f.refreshHook
	notPop := f.notPopulatedOnConcurrent
	refreshErr := f.refreshErr
	f.mu.Unlock()

	if hook != nil {
		hook(concurrently)
	}
	if concurrently && notPop {
		f.mu.Lock()
		f.notPopulatedOnConcurrent = false
		f.mu.Unlock()
		return errors.New("ERROR: CONCURRENTLY cannot refresh materialized view \"mv_fee_revenue_monthly\" that has not been populated")
	}
	return refreshErr
}

func (f *fakeFeeRevenueStore) MarkRefreshed(_ context.Context, at time.Time) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastRefreshed = at
	f.refreshedSet = true
	return nil
}

func (f *fakeFeeRevenueStore) LastRefreshedAt(_ context.Context) (time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.refreshedSet {
		return time.Time{}, false, nil
	}
	return f.lastRefreshed, true, nil
}

func (f *fakeFeeRevenueStore) calls() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]bool, len(f.refreshCalls))
	copy(out, f.refreshCalls)
	return out
}

func TestFeeRevenueRefreshConfig_Defaults(t *testing.T) {
	c := FeeRevenueRefreshConfig{}.withDefaults()
	if c.PollInterval != time.Hour {
		t.Errorf("PollInterval default = %v, want 1h", c.PollInterval)
	}
	if c.RefreshTimeout != 5*time.Minute {
		t.Errorf("RefreshTimeout default = %v, want 5m", c.RefreshTimeout)
	}
	if c.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout default = %v, want 30s", c.ShutdownTimeout)
	}
	if c.StalenessThreshold != 2*time.Hour {
		t.Errorf("StalenessThreshold default = %v, want 2h", c.StalenessThreshold)
	}
}

func TestFeeRevenueRefreshConfig_StalenessTracksPollInterval(t *testing.T) {
	// When only PollInterval is overridden, staleness defaults to 2x it.
	c := FeeRevenueRefreshConfig{PollInterval: 15 * time.Minute}.withDefaults()
	if c.StalenessThreshold != 30*time.Minute {
		t.Errorf("StalenessThreshold = %v, want 30m (2x poll)", c.StalenessThreshold)
	}
}

// TestRefreshOnce_FirstRefreshNonConcurrentThenConcurrent verifies the first
// refresh runs without CONCURRENTLY (view never populated) and subsequent
// refreshes run CONCURRENTLY so readers are never blocked.
func TestRefreshOnce_FirstRefreshNonConcurrentThenConcurrent(t *testing.T) {
	store := &fakeFeeRevenueStore{}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{}, nil)
	j.ctx = context.Background()

	j.refreshOnce()
	j.refreshOnce()
	j.refreshOnce()

	calls := store.calls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 refresh calls, got %d", len(calls))
	}
	if calls[0] != false {
		t.Errorf("first refresh should be non-concurrent, got concurrently=%v", calls[0])
	}
	if calls[1] != true || calls[2] != true {
		t.Errorf("subsequent refreshes should be concurrent, got %v", calls[1:])
	}

	stats := j.GetStats()
	if stats.Refreshed != 3 {
		t.Errorf("Refreshed = %d, want 3", stats.Refreshed)
	}
	if stats.ConsecutiveErr != 0 {
		t.Errorf("ConsecutiveErr = %d, want 0", stats.ConsecutiveErr)
	}
}

// TestRefreshOnce_NotPopulatedFallback verifies that if a CONCURRENTLY refresh
// is rejected because the view was never populated, the job falls back to a
// non-concurrent refresh and recovers.
func TestRefreshOnce_NotPopulatedFallback(t *testing.T) {
	store := &fakeFeeRevenueStore{notPopulatedOnConcurrent: true}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{}, nil)
	j.ctx = context.Background()

	// Force the job to believe the view is already populated so it attempts
	// CONCURRENTLY first.
	j.populated.Store(true)

	j.refreshOnce()

	calls := store.calls()
	if len(calls) != 2 {
		t.Fatalf("expected concurrent attempt + non-concurrent fallback (2 calls), got %d: %v", len(calls), calls)
	}
	if calls[0] != true {
		t.Errorf("first attempt should be concurrent, got %v", calls[0])
	}
	if calls[1] != false {
		t.Errorf("fallback should be non-concurrent, got %v", calls[1])
	}
	if got := j.GetStats(); got.Refreshed != 1 || got.Failed != 0 {
		t.Errorf("stats after fallback: refreshed=%d failed=%d, want 1/0", got.Refreshed, got.Failed)
	}
}

func TestRefreshOnce_RefreshErrorRecorded(t *testing.T) {
	store := &fakeFeeRevenueStore{refreshErr: errors.New("boom")}
	rec := &recordingLogger{}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{}, rec)
	j.ctx = context.Background()

	j.refreshOnce()

	stats := j.GetStats()
	if stats.Failed != 1 {
		t.Errorf("Failed = %d, want 1", stats.Failed)
	}
	if stats.ConsecutiveErr != 1 {
		t.Errorf("ConsecutiveErr = %d, want 1", stats.ConsecutiveErr)
	}
	if stats.LastRunError == "" {
		t.Error("LastRunError should be set")
	}
	if rec.count() == 0 {
		t.Error("logger.Error should have been called")
	}
	// The freshness timestamp must NOT advance on a failed refresh.
	if _, ok, _ := store.LastRefreshedAt(context.Background()); ok {
		t.Error("last_refreshed_at must not be set after a failed refresh")
	}
}

func TestRefreshOnce_MarkRefreshedErrorRecorded(t *testing.T) {
	store := &fakeFeeRevenueStore{markErr: errors.New("update failed")}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{}, nil)
	j.ctx = context.Background()

	j.refreshOnce()

	stats := j.GetStats()
	if stats.Failed != 1 {
		t.Errorf("Failed = %d, want 1 (mark-refreshed failure surfaces as error)", stats.Failed)
	}
	if stats.Refreshed != 0 {
		t.Errorf("Refreshed = %d, want 0", stats.Refreshed)
	}
}

func TestIsStale_Fresh(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeFeeRevenueStore{lastRefreshed: now.Add(-30 * time.Minute), refreshedSet: true}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{StalenessThreshold: 2 * time.Hour}, nil)

	stale, last, never, err := j.IsStale(context.Background(), now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stale || never {
		t.Errorf("expected fresh, got stale=%v never=%v", stale, never)
	}
	if !last.Equal(now.Add(-30 * time.Minute)) {
		t.Errorf("lastRefreshed mismatch: %v", last)
	}
}

// TestIsStale_StaleButServed covers the stale-but-served freshness check.
func TestIsStale_StaleButServed(t *testing.T) {
	now := time.Now().UTC()
	store := &fakeFeeRevenueStore{lastRefreshed: now.Add(-3 * time.Hour), refreshedSet: true}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{StalenessThreshold: 2 * time.Hour}, nil)

	stale, last, never, err := j.IsStale(context.Background(), now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stale {
		t.Error("expected stale=true")
	}
	if never {
		t.Error("expected never=false")
	}
	if last.IsZero() {
		t.Error("expected a non-zero lastRefreshed")
	}
}

func TestIsStale_NeverRefreshed(t *testing.T) {
	store := &fakeFeeRevenueStore{} // never refreshed
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{}, nil)

	stale, _, never, err := j.IsStale(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !never {
		t.Error("expected never=true")
	}
	if !stale {
		t.Error("never-refreshed data should be considered stale")
	}
}

func TestIsStale_StoreError(t *testing.T) {
	store := &errStore{err: errors.New("db down")}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{}, nil)

	if _, _, _, err := j.IsStale(context.Background(), time.Now()); err == nil {
		t.Error("expected error to propagate")
	}
}

// TestRefreshDuringLongRunningQuery simulates a long-running report read held
// open while a refresh runs. Because the refresh uses CONCURRENTLY (after the
// first populate), the reader is never blocked: it observes the prior data and
// completes independently of the refresh.
func TestRefreshDuringLongRunningQuery(t *testing.T) {
	store := &fakeFeeRevenueStore{}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{}, nil)
	j.ctx = context.Background()

	// First refresh populates the view (non-concurrent).
	j.refreshOnce()

	readerStarted := make(chan struct{})
	readerDone := make(chan struct{})
	var observedConcurrent bool

	// A long-running reader: it begins, signals, and stays "open" until the
	// refresh has been observed running concurrently.
	store.refreshHook = func(concurrently bool) {
		observedConcurrent = concurrently
		close(readerStarted)
		<-readerDone // refresh proceeds; in real Postgres CONCURRENTLY would not block this reader
	}

	go func() {
		j.refreshOnce()
	}()

	select {
	case <-readerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh did not start in time")
	}

	// The reader is still "in flight" here; releasing it lets the refresh finish.
	close(readerDone)

	// Give the refresh goroutine a moment to record stats.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if j.GetStats().Refreshed == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if !observedConcurrent {
		t.Error("refresh during a long-running reader must use CONCURRENTLY")
	}
	if got := j.GetStats().Refreshed; got != 2 {
		t.Errorf("Refreshed = %d, want 2", got)
	}
}

func TestFeeRevenueRefreshJob_StartStopHealth(t *testing.T) {
	store := &fakeFeeRevenueStore{}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{PollInterval: time.Hour}, nil)

	// Not started yet.
	if err := j.Health(); err == nil {
		t.Error("Health should fail before Start")
	}

	j.Start()
	// The startup refresh runs immediately; give it a beat.
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

// TestRefreshLoop_TickerFires verifies the loop refreshes again when the poll
// ticker fires (beyond the immediate startup refresh).
func TestRefreshLoop_TickerFires(t *testing.T) {
	store := &fakeFeeRevenueStore{}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{PollInterval: 20 * time.Millisecond}, nil)

	j.Start()
	defer j.Stop()

	// Wait for at least two refreshes (startup + at least one ticker tick).
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

// TestStop_ShutdownTimeout forces a refresh that outlives ShutdownTimeout so
// Stop returns a timeout error instead of blocking forever.
func TestStop_ShutdownTimeout(t *testing.T) {
	release := make(chan struct{})
	store := &fakeFeeRevenueStore{
		refreshHook: func(bool) { <-release }, // block until released
	}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{
		PollInterval:    time.Hour,
		ShutdownTimeout: 30 * time.Millisecond,
	}, nil)

	j.Start()
	// Let the startup refresh begin and block inside the hook.
	time.Sleep(20 * time.Millisecond)

	err := j.Stop()
	if err == nil {
		t.Error("expected shutdown timeout error while refresh is blocked")
	}
	close(release) // unblock the goroutine so the test can exit cleanly
}

// TestDoRefresh_FallbackAlsoFails covers the branch where the non-concurrent
// fallback after a not-populated CONCURRENTLY error itself fails.
func TestDoRefresh_FallbackAlsoFails(t *testing.T) {
	store := &fallbackFailStore{}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{}, nil)
	j.ctx = context.Background()
	j.populated.Store(true) // force CONCURRENTLY first

	err := j.doRefresh(context.Background())
	if err == nil {
		t.Fatal("expected error when fallback refresh fails")
	}
	if !strings.Contains(err.Error(), "non-concurrent fallback refresh") {
		t.Errorf("error should mention fallback, got %v", err)
	}
}

func TestFeeRevenueRefreshJob_StopWithoutStart(t *testing.T) {
	j := newFeeRevenueRefreshJob(&fakeFeeRevenueStore{}, FeeRevenueRefreshConfig{}, nil)
	if err := j.Stop(); err != nil {
		t.Errorf("Stop without Start should be a no-op, got %v", err)
	}
}

func TestHealth_UnhealthyAfterConsecutiveErrors(t *testing.T) {
	store := &fakeFeeRevenueStore{refreshErr: errors.New("boom")}
	j := newFeeRevenueRefreshJob(store, FeeRevenueRefreshConfig{}, nil)
	j.ctx = context.Background()
	j.running.Store(1) // pretend running so Health checks the error count

	for i := 0; i < 6; i++ {
		j.refreshOnce()
	}
	if err := j.Health(); err == nil {
		t.Error("Health should fail after >5 consecutive errors")
	}
}

func TestIsNotPopulatedErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"has not been populated", errors.New("materialized view has not been populated"), true},
		{"concurrently cannot refresh", errors.New("cannot refresh materialized view CONCURRENTLY"), true},
		{"unrelated", errors.New("connection reset"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNotPopulatedErr(tc.err); got != tc.want {
				t.Errorf("isNotPopulatedErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// fallbackFailStore fails the CONCURRENTLY refresh with a not-populated error
// and then fails the non-concurrent fallback too.
type fallbackFailStore struct{}

func (s *fallbackFailStore) Refresh(_ context.Context, concurrently bool) error {
	if concurrently {
		return errors.New("materialized view has not been populated")
	}
	return errors.New("fallback exec failed")
}
func (s *fallbackFailStore) MarkRefreshed(context.Context, time.Time) error { return nil }
func (s *fallbackFailStore) LastRefreshedAt(context.Context) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

// errStore returns an error from every method; used to test error propagation.
type errStore struct{ err error }

func (e *errStore) Refresh(context.Context, bool) error            { return e.err }
func (e *errStore) MarkRefreshed(context.Context, time.Time) error { return e.err }
func (e *errStore) LastRefreshedAt(context.Context) (time.Time, bool, error) {
	return time.Time{}, false, e.err
}

// recordingLogger counts Error calls.
type recordingLogger struct {
	mu sync.Mutex
	n  int
}

func (r *recordingLogger) Error(string, ...any) {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
}

func (r *recordingLogger) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}
