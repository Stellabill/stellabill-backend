package worker

import (
	"stellarbill-backend/internal/timeutil"
	"testing"
	"time"
)

// TestScheduler_UsesInjectedClockForTimestamps verifies that Scheduler derives
// CreatedAt/UpdatedAt (and the job ID's timestamp component) from an injected
// Clock rather than the system clock, so job creation timing is deterministic
// in tests.
func TestScheduler_UsesInjectedClockForTimestamps(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := timeutil.NewFakeClock(start)

	store := NewMemoryStore()
	s := NewScheduler(store)
	s.SetClock(clock)

	job, err := s.ScheduleCharge("sub-1", start.Add(time.Hour), 3, PriorityHigh)
	if err != nil {
		t.Fatalf("ScheduleCharge: %v", err)
	}
	if !job.CreatedAt.Equal(start) {
		t.Fatalf("CreatedAt = %v, want %v", job.CreatedAt, start)
	}
	if !job.UpdatedAt.Equal(start) {
		t.Fatalf("UpdatedAt = %v, want %v", job.UpdatedAt, start)
	}

	// Advancing the clock and scheduling again should reflect the new instant.
	clock.Advance(90 * time.Minute)
	want := start.Add(90 * time.Minute)

	job2, err := s.ScheduleInvoice("sub-2", want.Add(time.Hour), 3, PriorityNormal)
	if err != nil {
		t.Fatalf("ScheduleInvoice: %v", err)
	}
	if !job2.CreatedAt.Equal(want) {
		t.Fatalf("CreatedAt after advance = %v, want %v", job2.CreatedAt, want)
	}
}

// TestMemoryStore_LockExpiryIsDrivenByClock verifies that AcquireLock's TTL
// expiry is evaluated against the store's injected Clock, so lock takeover
// after expiry can be tested deterministically without sleeping.
func TestMemoryStore_LockExpiryIsDrivenByClock(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := timeutil.NewFakeClock(start)
	store := NewMemoryStoreWithClock(clock)

	ok, err := store.AcquireLock("job-1", "worker-a", 5*time.Minute)
	if err != nil || !ok {
		t.Fatalf("AcquireLock(worker-a) = %v, %v, want true, nil", ok, err)
	}

	// Before expiry, a different worker must not be able to acquire the lock.
	ok, err = store.AcquireLock("job-1", "worker-b", 5*time.Minute)
	if err != nil || ok {
		t.Fatalf("AcquireLock(worker-b) before expiry = %v, %v, want false, nil", ok, err)
	}

	// Advance past the TTL: the lock should now be takeable by another worker.
	clock.Advance(5*time.Minute + time.Second)

	ok, err = store.AcquireLock("job-1", "worker-b", 5*time.Minute)
	if err != nil || !ok {
		t.Fatalf("AcquireLock(worker-b) after expiry = %v, %v, want true, nil", ok, err)
	}
}
