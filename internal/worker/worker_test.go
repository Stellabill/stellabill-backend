package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// MockExecutor for testing
type MockExecutor struct {
	execFunc func(ctx context.Context, job *Job) error
	calls    int
}

func (m *MockExecutor) Execute(ctx context.Context, job *Job) error {
	m.calls++
	if m.execFunc != nil {
		return m.execFunc(ctx, job)
	}
	return nil
}

func TestWorker_StartStop(t *testing.T) {
	store := NewMemoryStore()
	executor := &MockExecutor{}
	config := DefaultConfig()
	config.PollInterval = 50 * time.Millisecond
	
	worker := NewWorker(store, executor, config)
	worker.Start()
	
	time.Sleep(100 * time.Millisecond)
	
	err := worker.Stop()
	if err != nil {
		t.Fatalf("Expected clean shutdown, got error: %v", err)
	}
}

func TestWorker_ProcessPendingJob(t *testing.T) {
	store := NewMemoryStore()
	executor := &MockExecutor{}
	config := DefaultConfig()
	config.PollInterval = 50 * time.Millisecond
	config.MaxAttempts = 3
	
	// Create a pending job
	job := &Job{
		ID:             "job-1",
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusPending,
		ScheduledAt:    time.Now().Add(-1 * time.Second),
		MaxAttempts:    config.MaxAttempts,
	}
	
	if err := store.Create(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	
	worker := NewWorker(store, executor, config)
	worker.Start()
	defer worker.Stop()
	
	// Wait for job to be processed
	time.Sleep(200 * time.Millisecond)
	
	// Verify job was executed
	if executor.calls != 1 {
		t.Errorf("Expected 1 execution, got %d", executor.calls)
	}
	
	// Verify job status
	updatedJob, err := store.Get("job-1")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}
	
	if updatedJob.Status != JobStatusCompleted {
		t.Errorf("Expected status %s, got %s", JobStatusCompleted, updatedJob.Status)
	}
	
	// Verify metrics
	metrics := worker.GetMetrics()
	if metrics.JobsProcessed != 1 {
		t.Errorf("Expected 1 job processed, got %d", metrics.JobsProcessed)
	}
	if metrics.JobsSucceeded != 1 {
		t.Errorf("Expected 1 job succeeded, got %d", metrics.JobsSucceeded)
	}
}

func TestWorker_RetryOnFailure(t *testing.T) {
	store := NewMemoryStore()
	
	attempts := 0
	executor := &MockExecutor{
		execFunc: func(ctx context.Context, job *Job) error {
			attempts++
			if attempts < 2 {
				return errors.New("temporary failure")
			}
			return nil
		},
	}
	
	config := DefaultConfig()
	config.PollInterval = 50 * time.Millisecond
	config.MaxAttempts = 3
	
	job := &Job{
		ID:             "job-retry",
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusPending,
		ScheduledAt:    time.Now().Add(-1 * time.Second),
		MaxAttempts:    config.MaxAttempts,
	}
	
	if err := store.Create(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	
	worker := NewWorker(store, executor, config)
	worker.Start()
	defer worker.Stop()
	
	// Wait for retries (first attempt + 1 retry with backoff)
	time.Sleep(2 * time.Second)
	
	if attempts < 2 {
		t.Errorf("Expected at least 2 attempts, got %d", attempts)
	}
	
	// Verify final job status
	updatedJob, err := store.Get("job-retry")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}
	
	if updatedJob.Status != JobStatusCompleted {
		t.Errorf("Expected status %s, got %s", JobStatusCompleted, updatedJob.Status)
	}
}

func TestWorker_DeadLetterAfterMaxAttempts(t *testing.T) {
	store := NewMemoryStore()
	
	executor := &MockExecutor{
		execFunc: func(ctx context.Context, job *Job) error {
			return errors.New("persistent failure")
		},
	}
	
	config := DefaultConfig()
	config.PollInterval = 50 * time.Millisecond
	config.MaxAttempts = 2
	
	job := &Job{
		ID:             "job-dead",
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusPending,
		ScheduledAt:    time.Now().Add(-1 * time.Second),
		MaxAttempts:    config.MaxAttempts,
	}
	
	if err := store.Create(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	
	worker := NewWorker(store, executor, config)
	worker.Start()
	defer worker.Stop()
	
	// Wait for all attempts (2 attempts with backoff)
	time.Sleep(3 * time.Second)
	
	// Verify job moved to dead-letter
	updatedJob, err := store.Get("job-dead")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}
	
	if updatedJob.Status != JobStatusDeadLetter {
		t.Errorf("Expected status %s, got %s", JobStatusDeadLetter, updatedJob.Status)
	}
	
	if updatedJob.Attempts != config.MaxAttempts {
		t.Errorf("Expected %d attempts, got %d", config.MaxAttempts, updatedJob.Attempts)
	}
	
	// Verify metrics
	metrics := worker.GetMetrics()
	if metrics.JobsDeadLettered != 1 {
		t.Errorf("Expected 1 dead-lettered job, got %d", metrics.JobsDeadLettered)
	}
}

func TestWorker_ConcurrentWorkers_NoDuplicateProcessing(t *testing.T) {
	store := NewMemoryStore()
	
	executionCount := 0
	var mu sync.Mutex
	
	executor := &MockExecutor{
		execFunc: func(ctx context.Context, job *Job) error {
			mu.Lock()
			executionCount++
			mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			return nil
		},
	}
	
	config1 := DefaultConfig()
	config1.WorkerID = "worker-1"
	config1.PollInterval = 50 * time.Millisecond
	
	config2 := DefaultConfig()
	config2.WorkerID = "worker-2"
	config2.PollInterval = 50 * time.Millisecond
	
	// Create a job
	job := &Job{
		ID:             "job-concurrent",
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusPending,
		ScheduledAt:    time.Now().Add(-1 * time.Second),
		MaxAttempts:    3,
	}
	
	if err := store.Create(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	
	// Start two workers
	worker1 := NewWorker(store, executor, config1)
	worker2 := NewWorker(store, executor, config2)
	
	worker1.Start()
	worker2.Start()
	
	// Wait for processing
	time.Sleep(300 * time.Millisecond)
	
	worker1.Stop()
	worker2.Stop()
	
	// Verify job was executed exactly once
	mu.Lock()
	count := executionCount
	mu.Unlock()
	
	if count != 1 {
		t.Errorf("Expected job to be executed exactly once, got %d executions", count)
	}
}

func TestWorker_SkipFutureJobs(t *testing.T) {
	store := NewMemoryStore()
	executor := &MockExecutor{}
	config := DefaultConfig()
	config.PollInterval = 50 * time.Millisecond
	
	// Create a future job
	job := &Job{
		ID:             "job-future",
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusPending,
		ScheduledAt:    time.Now().Add(10 * time.Second),
		MaxAttempts:    3,
	}
	
	if err := store.Create(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	
	worker := NewWorker(store, executor, config)
	worker.Start()
	defer worker.Stop()
	
	// Wait a bit
	time.Sleep(200 * time.Millisecond)
	
	// Verify job was NOT executed
	if executor.calls != 0 {
		t.Errorf("Expected 0 executions for future job, got %d", executor.calls)
	}
	
	// Verify job is still pending
	updatedJob, err := store.Get("job-future")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}
	
	if updatedJob.Status != JobStatusPending {
		t.Errorf("Expected status %s, got %s", JobStatusPending, updatedJob.Status)
	}
}

func TestWorker_GracefulShutdown(t *testing.T) {
	store := NewMemoryStore()
	
	executor := &MockExecutor{
		execFunc: func(ctx context.Context, job *Job) error {
			time.Sleep(200 * time.Millisecond)
			return nil
		},
	}
	
	config := DefaultConfig()
	config.PollInterval = 50 * time.Millisecond
	config.ShutdownTimeout = 1 * time.Second
	
	job := &Job{
		ID:             "job-shutdown",
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusPending,
		ScheduledAt:    time.Now().Add(-1 * time.Second),
		MaxAttempts:    3,
	}
	
	if err := store.Create(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	
	worker := NewWorker(store, executor, config)
	worker.Start()
	
	// Let job start processing
	time.Sleep(100 * time.Millisecond)
	
	// Stop should wait for job to complete
	err := worker.Stop()
	if err != nil {
		t.Fatalf("Expected graceful shutdown, got error: %v", err)
	}
	
	// Verify job completed
	updatedJob, err := store.Get("job-shutdown")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}
	
	if updatedJob.Status != JobStatusCompleted {
		t.Errorf("Expected status %s, got %s", JobStatusCompleted, updatedJob.Status)
	}
}

func TestWorker_ShutdownTimeout(t *testing.T) {
	store := NewMemoryStore()
	
	executor := &MockExecutor{
		execFunc: func(ctx context.Context, job *Job) error {
			time.Sleep(2 * time.Second)
			return nil
		},
	}
	
	config := DefaultConfig()
	config.PollInterval = 50 * time.Millisecond
	config.ShutdownTimeout = 100 * time.Millisecond
	
	job := &Job{
		ID:             "job-timeout",
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusPending,
		ScheduledAt:    time.Now().Add(-1 * time.Second),
		MaxAttempts:    3,
	}
	
	if err := store.Create(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	
	worker := NewWorker(store, executor, config)
	worker.Start()
	
	time.Sleep(100 * time.Millisecond)
	
	// Stop should timeout
	err := worker.Stop()
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
}
