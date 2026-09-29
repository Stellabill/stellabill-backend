package worker

import (
	"context"
	"errors"
	"stellarbill-backend/internal/timeutil"
	"sync"
	"testing"
	"time"
)

// fakePartitionRolloverStore implements partitionRolloverStore for tests.
type fakePartitionRolloverStore struct {
	mu              sync.Mutex
	lastRolloverAt  time.Time
	partitionExists map[string]bool
	partitions      []string
	createdCount    int
	detachedList    []string
}

func newFakePartitionStore() *fakePartitionRolloverStore {
	return &fakePartitionRolloverStore{
		lastRolloverAt:  time.Time{}, // zero = never
		partitionExists: make(map[string]bool),
	}
}

func (f *fakePartitionRolloverStore) LastRolloverAt(_ context.Context) (time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastRolloverAt.IsZero() {
		return time.Time{}, false, nil
	}
	return f.lastRolloverAt, true, nil
}

func (f *fakePartitionRolloverStore) MarkRolloverDone(_ context.Context, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastRolloverAt = at
	return nil
}

func (f *fakePartitionRolloverStore) PartitionExists(_ context.Context, parentTable, partitionName string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.partitionExists[partitionName], nil
}

func (f *fakePartitionRolloverStore) CreatePartition(_ context.Context, parentTable, partitionName, fromDate, toDate string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdCount++
	f.partitionExists[partitionName] = true
	f.partitions = append(f.partitions, partitionName)
	return nil
}

func (f *fakePartitionRolloverStore) DetachPartition(_ context.Context, parentTable, partitionName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detachedList = append(f.detachedList, partitionName)
	delete(f.partitionExists, partitionName)
	return nil
}

func (f *fakePartitionRolloverStore) ListPartitions(_ context.Context, parentTable string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.partitions))
	copy(out, f.partitions)
	return out, nil
}

type noopRolloverLogger struct{}

func (noopRolloverLogger) Error(msg string, keysAndValues ...any) {}

func TestNewPartitionRolloverJob(t *testing.T) {
	j := NewPartitionRolloverJob(nil, DefaultPartitionRolloverConfig(), nil)
	if j == nil {
		t.Fatal("expected non-nil job")
	}
	if j.config.PollInterval != 24*time.Hour {
		t.Errorf("expected default poll interval 24h, got %v", j.config.PollInterval)
	}
}

func TestPartitionRolloverJobLifecycle(t *testing.T) {
	store := newFakePartitionStore()
	clock := timeutil.NewFakeClock(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	cfg := DefaultPartitionRolloverConfig()
	cfg.PollInterval = 1 * time.Hour
	cfg.DetachThresholdMonths = 24

	j := newPartitionRolloverJob(store, cfg, noopRolloverLogger{})
	j.SetClock(clock)

	j.Start()
	defer j.Stop()

	if err := j.Health(); err != nil {
		t.Fatalf("expected healthy, got: %v", err)
	}

	stats := j.GetStats()
	if stats.LastRunError != "" {
		t.Errorf("expected no error, got: %s", stats.LastRunError)
	}
}

func TestPartitionRolloverCreatesNextMonth(t *testing.T) {
	store := newFakePartitionStore()
	clock := timeutil.NewFakeClock(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))
	cfg := DefaultPartitionRolloverConfig()
	cfg.Cooldown = 0 // disable cooldown for test

	j := newPartitionRolloverJob(store, cfg, noopRolloverLogger{})
	j.SetClock(clock)

	partitionName := j.partitionName(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if partitionName != "statements_p2026_08" {
		t.Fatalf("unexpected partition name: %s", partitionName)
	}

	// Run rollover
	j.rolloverOnce()

	// Verify partition was created
	exists, _ := store.PartitionExists(context.Background(), cfg.ParentTable, partitionName)
	if !exists {
		t.Error("expected partition statements_p2026_08 to exist after rollover")
	}

	if store.createdCount != 1 {
		t.Errorf("expected 1 partition created, got %d", store.createdCount)
	}
}

func TestPartitionRolloverSkipsExistingPartition(t *testing.T) {
	store := newFakePartitionStore()
	partitionName := "statements_p2026_08"

	// Pre-create the partition
	store.partitionExists[partitionName] = true
	store.partitions = append(store.partitions, partitionName)

	clock := timeutil.NewFakeClock(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))
	cfg := DefaultPartitionRolloverConfig()
	cfg.Cooldown = 0

	j := newPartitionRolloverJob(store, cfg, noopRolloverLogger{})
	j.SetClock(clock)

	j.rolloverOnce()

	// No new partitions should be created
	if store.createdCount != 0 {
		t.Errorf("expected 0 partitions created (already exists), got %d", store.createdCount)
	}
}

func TestPartitionRolloverDetachesOldPartitions(t *testing.T) {
	store := newFakePartitionStore()
	clock := timeutil.NewFakeClock(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))
	cfg := DefaultPartitionRolloverConfig()
	cfg.Cooldown = 0
	cfg.DetachThresholdMonths = 12 // detach partitions older than 12 months

	// Pre-create some partitions (including old ones)
	oldPartitions := []string{
		"statements_p2024_01",
		"statements_p2024_06",
		"statements_p2025_01",
	}
	for _, p := range oldPartitions {
		store.partitionExists[p] = true
		store.partitions = append(store.partitions, p)
	}

	j := newPartitionRolloverJob(store, cfg, noopRolloverLogger{})
	j.SetClock(clock)

	j.rolloverOnce()

	// Partitions from 2024 should be detached (older than 12 months before July 2026)
	if len(store.detachedList) == 0 {
		t.Error("expected some partitions to be detached")
	}
}

func TestPartitionRolloverIdempotencyCooldown(t *testing.T) {
	store := newFakePartitionStore()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	clock := timeutil.NewFakeClock(now)
	cfg := DefaultPartitionRolloverConfig()
	cfg.Cooldown = 1 * time.Hour

	// Mark last rollover as 30 minutes ago (within cooldown)
	store.lastRolloverAt = now.Add(-30 * time.Minute)

	j := newPartitionRolloverJob(store, cfg, noopRolloverLogger{})
	j.SetClock(clock)

	j.rolloverOnce()

	// Nothing should happen since we're within cooldown
	if store.createdCount != 0 {
		t.Errorf("expected 0 created within cooldown, got %d", store.createdCount)
	}
}

func TestPartitionRolloverSkipsCooldownWhenExpired(t *testing.T) {
	store := newFakePartitionStore()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	clock := timeutil.NewFakeClock(now)
	cfg := DefaultPartitionRolloverConfig()
	cfg.Cooldown = 1 * time.Hour

	// Mark last rollover as 2 hours ago (outside cooldown)
	store.lastRolloverAt = now.Add(-2 * time.Hour)

	j := newPartitionRolloverJob(store, cfg, noopRolloverLogger{})
	j.SetClock(clock)

	j.rolloverOnce()

	// Should create the next partition since cooldown has expired
	if store.createdCount == 0 {
		t.Error("expected partitions to be created after cooldown expired")
	}
}

func TestPartitionRolloverIgnoresDefaultPartition(t *testing.T) {
	store := newFakePartitionStore()
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	clock := timeutil.NewFakeClock(now)
	cfg := DefaultPartitionRolloverConfig()
	cfg.Cooldown = 0
	cfg.DetachThresholdMonths = 12

	// Add a default partition
	defaultPart := "statements_p_default"
	store.partitions = append(store.partitions, defaultPart)
	store.partitionExists[defaultPart] = true

	// Also add an old partition that should be detached
	store.partitions = append(store.partitions, "statements_p2024_01")
	store.partitionExists["statements_p2024_01"] = true

	j := newPartitionRolloverJob(store, cfg, noopRolloverLogger{})
	j.SetClock(clock)

	j.rolloverOnce()

	// The default partition should never be detached
	for _, p := range store.detachedList {
		if p == defaultPart {
			t.Error("default partition should never be detached")
		}
	}

	// The old partition should be detached
	foundOld := false
	for _, p := range store.detachedList {
		if p == "statements_p2024_01" {
			foundOld = true
			break
		}
	}
	if !foundOld {
		t.Error("expected old partition statements_p2024_01 to be detached")
	}
}

func TestPartitionRolloverNameGeneration(t *testing.T) {
	j := &PartitionRolloverJob{
		config: DefaultPartitionRolloverConfig(),
	}

	tests := []struct {
		month    time.Time
		expected string
	}{
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "statements_p2026_01"},
		{time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), "statements_p2026_07"},
		{time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), "statements_p2026_12"},
		{time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), "statements_p2027_01"},
	}

	for _, tt := range tests {
		name := j.partitionName(tt.month)
		if name != tt.expected {
			t.Errorf("partitionName(%v) = %s, want %s", tt.month, name, tt.expected)
		}
	}
}

func TestPartitionRolloverParseDate(t *testing.T) {
	j := &PartitionRolloverJob{
		config: DefaultPartitionRolloverConfig(),
	}

	tests := []struct {
		name      string
		wantOK    bool
		wantMonth time.Month
		wantYear  int
	}{
		{"statements_p2026_01", true, time.January, 2026},
		{"statements_p2026_07", true, time.July, 2026},
		{"statements_p2026_12", true, time.December, 2026},
		{"statements_p_default", false, 0, 0},
		{"not_a_partition", false, 0, 0},
		{"", false, 0, 0},
	}

	for _, tt := range tests {
		tm, ok := j.parsePartitionDate(tt.name)
		if ok != tt.wantOK {
			t.Errorf("parsePartitionDate(%q) ok=%v, want %v", tt.name, ok, tt.wantOK)
		}
		if ok {
			if tm.Year() != tt.wantYear || tm.Month() != tt.wantMonth {
				t.Errorf("parsePartitionDate(%q) = %d-%d, want %d-%d",
					tt.name, tm.Year(), tm.Month(), tt.wantYear, tt.wantMonth)
			}
		}
	}
}

func TestPartitionRolloverDefaultConfig(t *testing.T) {
	cfg := DefaultPartitionRolloverConfig()
	if cfg.PollInterval != 24*time.Hour {
		t.Errorf("expected PollInterval 24h, got %v", cfg.PollInterval)
	}
	if cfg.LookaheadMonths != 1 {
		t.Errorf("expected LookaheadMonths 1, got %d", cfg.LookaheadMonths)
	}
	if cfg.DetachThresholdMonths != 24 {
		t.Errorf("expected DetachThresholdMonths 24, got %d", cfg.DetachThresholdMonths)
	}
	if cfg.ParentTable != "statements_partitioned" {
		t.Errorf("expected ParentTable statements_partitioned, got %s", cfg.ParentTable)
	}
	if cfg.Cooldown != 1*time.Hour {
		t.Errorf("expected Cooldown 1h, got %v", cfg.Cooldown)
	}
}

func TestPartitionRolloverStoreError(t *testing.T) {
	// Store that returns errors on every operation
	errStore := &errRolloverStore{}
	clock := timeutil.NewFakeClock(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))
	cfg := DefaultPartitionRolloverConfig()
	cfg.Cooldown = 0

	j := newPartitionRolloverJob(errStore, cfg, noopRolloverLogger{})
	j.SetClock(clock)

	j.rolloverOnce()

	stats := j.GetStats()
	if stats.ConsecutiveErr == 0 {
		t.Error("expected consecutive errors after store failure")
	}
}

type errRolloverStore struct{}

func (e *errRolloverStore) LastRolloverAt(_ context.Context) (time.Time, bool, error) {
	return time.Time{}, false, errors.New("store error")
}
func (e *errRolloverStore) MarkRolloverDone(_ context.Context, _ time.Time) error {
	return errors.New("store error")
}
func (e *errRolloverStore) PartitionExists(_ context.Context, _, _ string) (bool, error) {
	return false, errors.New("store error")
}
func (e *errRolloverStore) CreatePartition(_ context.Context, _, _, _, _ string) error {
	return errors.New("store error")
}
func (e *errRolloverStore) DetachPartition(_ context.Context, _, _ string) error {
	return errors.New("store error")
}
func (e *errRolloverStore) ListPartitions(_ context.Context, _ string) ([]string, error) {
	return nil, errors.New("store error")
}

func TestPartitionRolloverConcurrentAccess(t *testing.T) {
	store := newFakePartitionStore()
	clock := timeutil.NewFakeClock(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))
	cfg := DefaultPartitionRolloverConfig()
	cfg.Cooldown = 0

	j := newPartitionRolloverJob(store, cfg, noopRolloverLogger{})
	j.SetClock(clock)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j.rolloverOnce()
		}()
	}
	wg.Wait()

	// Should have at least one partition created (the first call)
	// Subsequent calls should skip since partition already exists
	if store.createdCount < 1 {
		t.Errorf("expected at least 1 partition created, got %d", store.createdCount)
	}
}
