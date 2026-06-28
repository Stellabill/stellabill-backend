package worker

import (
	"context"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustCreate(t *testing.T, store JobStore, job *Job) {
	t.Helper()
	if err := store.Create(job); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func now() time.Time { return time.Now() }

func pendingJob(id string, priority Priority, scheduledAt time.Time) *Job {
	return &Job{
		ID:          id,
		Type:        "test",
		Status:      JobStatusPending,
		Priority:    priority,
		ScheduledAt: scheduledAt,
		MaxAttempts: 3,
		CreatedAt:   now(),
		UpdatedAt:   now(),
	}
}

// ---------------------------------------------------------------------------
// Schedule creation tests (basic smoke)
// ---------------------------------------------------------------------------

func TestScheduler_ScheduleCharge(t *testing.T) {
	store := NewMemoryStore()
	s := NewScheduler(store)
	job, err := s.ScheduleCharge("sub-1", now(), 3, PriorityHigh)
	if err != nil {
		t.Fatalf("ScheduleCharge: %v", err)
	}
	if job.Priority != PriorityHigh {
		t.Errorf("got priority %d, want %d", job.Priority, PriorityHigh)
	}
	if job.Type != "charge" {
		t.Errorf("got type %q, want %q", job.Type, "charge")
	}
	if n := store.QueueDepth(); n != 1 {
		t.Errorf("expected queue depth 1, got %d", n)
	}
}

func TestScheduler_ScheduleInvoice(t *testing.T) {
	store := NewMemoryStore()
	s := NewScheduler(store)
	job, err := s.ScheduleInvoice("sub-1", now(), 3, PriorityNormal)
	if err != nil {
		t.Fatalf("ScheduleInvoice: %v", err)
	}
	if job.Priority != PriorityNormal {
		t.Errorf("got priority %d, want %d", job.Priority, PriorityNormal)
	}
}

func TestScheduler_ScheduleReminder(t *testing.T) {
	store := NewMemoryStore()
	s := NewScheduler(store)
	job, err := s.ScheduleReminder("sub-1", now(), 3, PriorityLow)
	if err != nil {
		t.Fatalf("ScheduleReminder: %v", err)
	}
	if job.Priority != PriorityLow {
		t.Errorf("got priority %d, want %d", job.Priority, PriorityLow)
	}
}

// ---------------------------------------------------------------------------
// Priority ordering – ListPending sorts by priority then time
// ---------------------------------------------------------------------------

func TestMemoryStore_SortByPriorityThenTime(t *testing.T) {
	store := NewMemoryStore()
	mustCreate(t, store, pendingJob("a", PriorityNormal, now().Add(1*time.Second)))
	mustCreate(t, store, pendingJob("b", PriorityHigh, now().Add(3*time.Second)))
	mustCreate(t, store, pendingJob("c", PriorityLow, now().Add(2*time.Second)))
	mustCreate(t, store, pendingJob("d", PriorityHigh, now().Add(1*time.Second)))

	jobs, err := store.ListPending(10)
	if err != nil {
		t.Fatal(err)
	}

	// Expected order: high first (by time), then normal, then low
	expect := []string{"d", "b", "a", "c"}
	for i, job := range jobs {
		if job.ID != expect[i] {
			t.Errorf("position %d: got %q, want %q", i, job.ID, expect[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Weighted round-robin distribution
// ---------------------------------------------------------------------------

func TestScheduler_WeightedRoundRobin(t *testing.T) {
	store := NewMemoryStore()
	s := NewScheduler(store)
	s.SetStarvationLimit(100) // disable starvation guard

	// Fill each lane with many jobs
	for i := 0; i < 10; i++ {
		mustCreate(t, store, pendingJob("h"+itos(i), PriorityHigh, now()))
		mustCreate(t, store, pendingJob("n"+itos(i), PriorityNormal, now()))
		mustCreate(t, store, pendingJob("l"+itos(i), PriorityLow, now()))
	}

	picks := map[Priority]int{PriorityHigh: 0, PriorityNormal: 0, PriorityLow: 0}
	totalPicks := 60 // 6 cycles of the 3:2:1 pattern → 30 expected high, 20 normal, 10 low

	for i := 0; i < totalPicks; i++ {
		job, err := s.Next()
		if err != nil {
			t.Fatalf("Next at pick %d: %v", i, err)
		}
		if job == nil {
			t.Fatalf("unexpected nil job at pick %d", i)
		}
		picks[job.Priority]++
	}

	// The distribution should roughly follow the 3:2:1 ratio
	highRatio := float64(picks[PriorityHigh]) / float64(totalPicks)
	normalRatio := float64(picks[PriorityNormal]) / float64(totalPicks)
	lowRatio := float64(picks[PriorityLow]) / float64(totalPicks)

	t.Logf("Picks: high=%d (%.1f%%), normal=%d (%.1f%%), low=%d (%.1f%%)",
		picks[PriorityHigh], highRatio*100,
		picks[PriorityNormal], normalRatio*100,
		picks[PriorityLow], lowRatio*100)

	// With 60 picks and 10 jobs per lane, the ratio should be close to 3:2:1.
	// Allow ±15 % absolute tolerance since RR cycles modulo remaining jobs.
	if highRatio < 0.35 || highRatio > 0.65 {
		t.Errorf("high ratio %.2f out of expected range [0.35, 0.65]", highRatio)
	}
	if normalRatio < 0.15 || normalRatio > 0.45 {
		t.Errorf("normal ratio %.2f out of expected range [0.15, 0.45]", normalRatio)
	}
	if lowRatio < 0.05 || lowRatio > 0.25 {
		t.Errorf("low ratio %.2f out of expected range [0.05, 0.25]", lowRatio)
	}
}

// ---------------------------------------------------------------------------
// Starvation guard – forces a low-lane pick when high/normal dominate
// ---------------------------------------------------------------------------

func TestScheduler_StarvationGuard(t *testing.T) {
	store := NewMemoryStore()
	s := NewScheduler(store)
	s.SetStarvationLimit(5) // force low after 5 consecutive high/normal picks

	// Fill only high and low lanes
	for i := 0; i < 20; i++ {
		mustCreate(t, store, pendingJob("h"+itos(i), PriorityHigh, now()))
	}
	for i := 0; i < 20; i++ {
		mustCreate(t, store, pendingJob("l"+itos(i), PriorityLow, now()))
	}

	// Pick 30 jobs. The starvation guard should prevent the low lane from being
	// completely ignored. Since weight is 3:2:1 and limit is 5, every 5th pick
	// or so should be low.
	pickedLow := false
	for i := 0; i < 30; i++ {
		job, err := s.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if job == nil {
			t.Fatalf("nil job at pick %d", i)
		}
		if job.Priority == PriorityLow {
			pickedLow = true
		}
	}

	if !pickedLow {
		t.Fatal("starvation guard never picked a low-priority job")
	}
}

// ---------------------------------------------------------------------------
// Fallback: empty lane does not block
// ---------------------------------------------------------------------------

func TestScheduler_EmptyLanesFallback(t *testing.T) {
	store := NewMemoryStore()
	s := NewScheduler(store)

	// Only low-priority jobs exist
	for i := 0; i < 5; i++ {
		mustCreate(t, store, pendingJob("l"+itos(i), PriorityLow, now()))
	}

	// The weighted RR may try high or normal first, but should fall back to low
	for i := 0; i < 10; i++ {
		job, err := s.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if job == nil {
			break
		}
		if job.Priority != PriorityLow {
			t.Errorf("expected low priority, got %d", job.Priority)
		}
	}
}

// ---------------------------------------------------------------------------
// Next returns nil when no pending jobs exist
// ---------------------------------------------------------------------------

func TestScheduler_NextReturnsNilWhenEmpty(t *testing.T) {
	store := NewMemoryStore()
	s := NewScheduler(store)

	job, err := s.Next()
	if err != nil {
		t.Fatalf("Next on empty store: %v", err)
	}
	if job != nil {
		t.Fatalf("expected nil, got %+v", job)
	}
}

// ---------------------------------------------------------------------------
// LaneDepth tracks per-priority depth
// ---------------------------------------------------------------------------

func TestMemoryStore_LaneDepth(t *testing.T) {
	store := NewMemoryStore()

	mustCreate(t, store, pendingJob("h1", PriorityHigh, now()))
	mustCreate(t, store, pendingJob("h2", PriorityHigh, now()))
	mustCreate(t, store, pendingJob("n1", PriorityNormal, now()))
	mustCreate(t, store, pendingJob("l1", PriorityLow, now()))

	tests := []struct {
		p Priority
		w int
	}{
		{PriorityHigh, 2},
		{PriorityNormal, 1},
		{PriorityLow, 1},
	}
	for _, tt := range tests {
		if got := store.LaneDepth(tt.p); got != tt.w {
			t.Errorf("LaneDepth(%d) = %d, want %d", tt.p, got, tt.w)
		}
	}

	// Future-scheduled jobs should not count
	future := now().Add(1 * time.Hour)
	mustCreate(t, store, pendingJob("h3", PriorityHigh, future))
	if got := store.LaneDepth(PriorityHigh); got != 2 {
		t.Errorf("LaneDepth(high) after future job = %d, want 2", got)
	}
}

// ---------------------------------------------------------------------------
// Scheduler picks from highest lane when weighted lane is empty
// ---------------------------------------------------------------------------

func TestScheduler_FallsBackToStrictPriority(t *testing.T) {
	store := NewMemoryStore()
	s := NewScheduler(store)

	// Only high and low, no normal
	mustCreate(t, store, pendingJob("h1", PriorityHigh, now()))
	mustCreate(t, store, pendingJob("l1", PriorityLow, now()))

	// First pick might try high (weighted RR) or normal (weighted RR then fallback)
	job1, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if job1 == nil {
		t.Fatal("expected job1")
	}
	// Should get the high job eventually (or on first try)
	_ = job1

	// After exhausting high, should get low
	var gotLow bool
	for i := 0; i < 5; i++ {
		job, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			break
		}
		if job.Priority == PriorityLow {
			gotLow = true
		}
	}
	if !gotLow {
		t.Fatal("never got low-priority job despite being only remaining lane")
	}
}

// ---------------------------------------------------------------------------
// Metrics reflect per-lane depth and picked totals
// ---------------------------------------------------------------------------

func TestWorkerMetrics_LaneDepth(t *testing.T) {
	store := NewMemoryStore()
	s := NewScheduler(store)
	_ = s // we test via worker

	executor := &noopExecutor{}
	cfg := DefaultConfig()
	cfg.PollInterval = 10 * time.Second // stop polling
	w := NewWorker(store, executor, cfg)

	mustCreate(t, store, pendingJob("h1", PriorityHigh, now()))
	mustCreate(t, store, pendingJob("h2", PriorityHigh, now()))
	mustCreate(t, store, pendingJob("n1", PriorityNormal, now()))

	metrics := w.GetMetrics()
	if metrics.LaneDepth[PriorityHigh] != 2 {
		t.Errorf("LaneDepth[high] = %d, want 2", metrics.LaneDepth[PriorityHigh])
	}
	if metrics.LaneDepth[PriorityNormal] != 1 {
		t.Errorf("LaneDepth[normal] = %d, want 1", metrics.LaneDepth[PriorityNormal])
	}
	if metrics.LaneDepth[PriorityLow] != 0 {
		t.Errorf("LaneDepth[low] = %d, want 0", metrics.LaneDepth[PriorityLow])
	}
}

// ---------------------------------------------------------------------------
// Custom lane weights
// ---------------------------------------------------------------------------

func TestScheduler_CustomWeights(t *testing.T) {
	store := NewMemoryStore()
	s := NewScheduler(store)

	// Override weights so only high and low are picked (normal weight = 0)
	s.SetWeights(map[Priority]int{
		PriorityHigh:   1,
		PriorityNormal: 0,
		PriorityLow:    1,
	})

	for i := 0; i < 5; i++ {
		mustCreate(t, store, pendingJob("h"+itos(i), PriorityHigh, now()))
		mustCreate(t, store, pendingJob("l"+itos(i), PriorityLow, now()))
	}

	// The zero-weight normal lane should never be picked via weighted RR.
	// The starvation guard should not cause issues since the total weight is 2.
	picks := map[Priority]int{PriorityHigh: 0, PriorityNormal: 0, PriorityLow: 0}
	for i := 0; i < 10; i++ {
		job, err := s.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if job == nil {
			break
		}
		picks[job.Priority]++
	}

	if picks[PriorityNormal] > 0 {
		t.Errorf("normal lane was picked %d times despite zero weight", picks[PriorityNormal])
	}
	t.Logf("Custom-weight picks: high=%d, normal=%d, low=%d",
		picks[PriorityHigh], picks[PriorityNormal], picks[PriorityLow])
}

// ---------------------------------------------------------------------------
// Concurrent safety (race detection)
// ---------------------------------------------------------------------------

func TestScheduler_ConcurrentSafe(t *testing.T) {
	store := NewMemoryStore()
	s := NewScheduler(store)

	for i := 0; i < 30; i++ {
		mustCreate(t, store, pendingJob("j"+itos(i), PriorityNormal, now()))
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 6; j++ {
				job, err := s.Next()
				if err != nil {
					return
				}
				if job != nil {
					_ = job.ID
				}
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func itos(i int) string {
	return string(rune('a' + i))
}

// noopExecutor implements JobExecutor with no-op execution.
type noopExecutor struct{}

func (n *noopExecutor) Execute(_ context.Context, _ *Job) error { return nil }
