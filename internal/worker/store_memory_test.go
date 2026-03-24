package worker

import (
	"fmt"
	"testing"
	"time"
)

func TestMemoryStore_CreateAndGet(t *testing.T) {
	store := NewMemoryStore()
	
	job := &Job{
		ID:             "test-1",
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusPending,
		ScheduledAt:    time.Now(),
		MaxAttempts:    3,
		Payload:        map[string]interface{}{"amount": 100},
	}
	
	err := store.Create(job)
	if err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	
	retrieved, err := store.Get("test-1")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}
	
	if retrieved.ID != job.ID {
		t.Errorf("Expected ID %s, got %s", job.ID, retrieved.ID)
	}
	if retrieved.SubscriptionID != job.SubscriptionID {
		t.Errorf("Expected SubscriptionID %s, got %s", job.SubscriptionID, retrieved.SubscriptionID)
	}
	if retrieved.Status != job.Status {
		t.Errorf("Expected Status %s, got %s", job.Status, retrieved.Status)
	}
}

func TestMemoryStore_CreateWithoutID(t *testing.T) {
	store := NewMemoryStore()
	
	job := &Job{
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusPending,
	}
	
	err := store.Create(job)
	if err == nil {
		t.Error("Expected error for job without ID, got nil")
	}
}

func TestMemoryStore_GetNonExistent(t *testing.T) {
	store := NewMemoryStore()
	
	_, err := store.Get("non-existent")
	if err != ErrJobNotFound {
		t.Errorf("Expected ErrJobNotFound, got %v", err)
	}
}

func TestMemoryStore_Update(t *testing.T) {
	store := NewMemoryStore()
	
	job := &Job{
		ID:             "test-update",
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusPending,
		ScheduledAt:    time.Now(),
		MaxAttempts:    3,
	}
	
	if err := store.Create(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	
	// Update job
	job.Status = JobStatusCompleted
	job.Attempts = 1
	
	if err := store.Update(job); err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}
	
	// Verify update
	updated, err := store.Get("test-update")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}
	
	if updated.Status != JobStatusCompleted {
		t.Errorf("Expected status %s, got %s", JobStatusCompleted, updated.Status)
	}
	if updated.Attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", updated.Attempts)
	}
}

func TestMemoryStore_UpdateNonExistent(t *testing.T) {
	store := NewMemoryStore()
	
	job := &Job{
		ID:     "non-existent",
		Status: JobStatusCompleted,
	}
	
	err := store.Update(job)
	if err != ErrJobNotFound {
		t.Errorf("Expected ErrJobNotFound, got %v", err)
	}
}

func TestMemoryStore_ListPending(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now()
	
	// Create mix of jobs
	jobs := []*Job{
		{
			ID:          "pending-1",
			Status:      JobStatusPending,
			ScheduledAt: now.Add(-10 * time.Second),
			MaxAttempts: 3,
		},
		{
			ID:          "pending-2",
			Status:      JobStatusPending,
			ScheduledAt: now.Add(-5 * time.Second),
			MaxAttempts: 3,
		},
		{
			ID:          "future",
			Status:      JobStatusPending,
			ScheduledAt: now.Add(10 * time.Second),
			MaxAttempts: 3,
		},
		{
			ID:          "completed",
			Status:      JobStatusCompleted,
			ScheduledAt: now.Add(-10 * time.Second),
			MaxAttempts: 3,
		},
	}
	
	for _, job := range jobs {
		if err := store.Create(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
	}
	
	pending, err := store.ListPending(10)
	if err != nil {
		t.Fatalf("Failed to list pending: %v", err)
	}
	
	// Should return only past pending jobs, sorted by scheduled time
	if len(pending) != 2 {
		t.Errorf("Expected 2 pending jobs, got %d", len(pending))
	}
	
	// Verify sorting (oldest first)
	if len(pending) == 2 {
		if pending[0].ID != "pending-1" {
			t.Errorf("Expected first job to be pending-1, got %s", pending[0].ID)
		}
		if pending[1].ID != "pending-2" {
			t.Errorf("Expected second job to be pending-2, got %s", pending[1].ID)
		}
	}
}

func TestMemoryStore_ListPendingWithLimit(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now()
	
	// Create multiple pending jobs
	for i := 0; i < 5; i++ {
		job := &Job{
			ID:          fmt.Sprintf("job-%d", i),
			Status:      JobStatusPending,
			ScheduledAt: now.Add(-1 * time.Second),
			MaxAttempts: 3,
		}
		if err := store.Create(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
	}
	
	pending, err := store.ListPending(3)
	if err != nil {
		t.Fatalf("Failed to list pending: %v", err)
	}
	
	if len(pending) != 3 {
		t.Errorf("Expected 3 pending jobs (limit), got %d", len(pending))
	}
}

func TestMemoryStore_ListDeadLetter(t *testing.T) {
	store := NewMemoryStore()
	
	jobs := []*Job{
		{
			ID:          "dead-1",
			Status:      JobStatusDeadLetter,
			ScheduledAt: time.Now(),
			MaxAttempts: 3,
		},
		{
			ID:          "dead-2",
			Status:      JobStatusDeadLetter,
			ScheduledAt: time.Now(),
			MaxAttempts: 3,
		},
		{
			ID:          "pending",
			Status:      JobStatusPending,
			ScheduledAt: time.Now(),
			MaxAttempts: 3,
		},
	}
	
	for _, job := range jobs {
		if err := store.Create(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
	}
	
	deadLetters, err := store.ListDeadLetter()
	if err != nil {
		t.Fatalf("Failed to list dead letters: %v", err)
	}
	
	if len(deadLetters) != 2 {
		t.Errorf("Expected 2 dead-letter jobs, got %d", len(deadLetters))
	}
}

func TestMemoryStore_AcquireLock(t *testing.T) {
	store := NewMemoryStore()
	
	// First worker acquires lock
	acquired, err := store.AcquireLock("job-1", "worker-1", 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}
	if !acquired {
		t.Error("Expected lock to be acquired")
	}
	
	// Second worker tries to acquire same lock
	acquired, err = store.AcquireLock("job-1", "worker-2", 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}
	if acquired {
		t.Error("Expected lock to NOT be acquired by second worker")
	}
	
	// Same worker can renew lock
	acquired, err = store.AcquireLock("job-1", "worker-1", 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to renew lock: %v", err)
	}
	if !acquired {
		t.Error("Expected same worker to renew lock")
	}
}

func TestMemoryStore_LockExpiration(t *testing.T) {
	store := NewMemoryStore()
	
	// Acquire lock with short TTL
	acquired, err := store.AcquireLock("job-1", "worker-1", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}
	if !acquired {
		t.Error("Expected lock to be acquired")
	}
	
	// Wait for lock to expire
	time.Sleep(150 * time.Millisecond)
	
	// Different worker should be able to acquire expired lock
	acquired, err = store.AcquireLock("job-1", "worker-2", 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to acquire expired lock: %v", err)
	}
	if !acquired {
		t.Error("Expected expired lock to be acquired by different worker")
	}
}

func TestMemoryStore_ReleaseLock(t *testing.T) {
	store := NewMemoryStore()
	
	// Acquire and release lock
	store.AcquireLock("job-1", "worker-1", 1*time.Second)
	
	err := store.ReleaseLock("job-1", "worker-1")
	if err != nil {
		t.Fatalf("Failed to release lock: %v", err)
	}
	
	// Another worker should be able to acquire
	acquired, err := store.AcquireLock("job-1", "worker-2", 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to acquire lock after release: %v", err)
	}
	if !acquired {
		t.Error("Expected lock to be acquired after release")
	}
}

func TestMemoryStore_ReleaseLockNotHeld(t *testing.T) {
	store := NewMemoryStore()
	
	store.AcquireLock("job-1", "worker-1", 1*time.Second)
	
	// Different worker tries to release
	err := store.ReleaseLock("job-1", "worker-2")
	if err != ErrLockNotHeld {
		t.Errorf("Expected ErrLockNotHeld, got %v", err)
	}
}

func TestMemoryStore_ReleaseLockNonExistent(t *testing.T) {
	store := NewMemoryStore()
	
	// Releasing non-existent lock should not error
	err := store.ReleaseLock("non-existent", "worker-1")
	if err != nil {
		t.Errorf("Expected nil error for non-existent lock, got %v", err)
	}
}
