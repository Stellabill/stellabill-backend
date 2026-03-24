package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Config holds worker configuration
type Config struct {
	WorkerID        string
	PollInterval    time.Duration
	LockTTL         time.Duration
	MaxAttempts     int
	BatchSize       int
	ShutdownTimeout time.Duration
}

// DefaultConfig returns sensible defaults for the worker
func DefaultConfig() Config {
	return Config{
		WorkerID:        generateWorkerID(),
		PollInterval:    5 * time.Second,
		LockTTL:         30 * time.Second,
		MaxAttempts:     3,
		BatchSize:       10,
		ShutdownTimeout: 30 * time.Second,
	}
}

// JobExecutor defines the interface for executing billing jobs
type JobExecutor interface {
	Execute(ctx context.Context, job *Job) error
}

// Worker manages background billing job scheduling and execution
type Worker struct {
	config   Config
	store    JobStore
	executor JobExecutor
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	metrics  *Metrics
}

// Metrics tracks worker execution statistics
type Metrics struct {
	mu               sync.RWMutex
	JobsProcessed    int64
	JobsSucceeded    int64
	JobsFailed       int64
	JobsDeadLettered int64
	LastPollTime     time.Time
}

// NewWorker creates a new billing worker
func NewWorker(store JobStore, executor JobExecutor, config Config) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{
		config:   config,
		store:    store,
		executor: executor,
		ctx:      ctx,
		cancel:   cancel,
		metrics:  &Metrics{},
	}
}

// Start begins the worker's scheduling loop
func (w *Worker) Start() {
	w.wg.Add(1)
	go w.schedulerLoop()
	log.Printf("Worker %s started with poll interval %v", w.config.WorkerID, w.config.PollInterval)
}

// Stop gracefully shuts down the worker
func (w *Worker) Stop() error {
	log.Printf("Worker %s shutting down...", w.config.WorkerID)
	w.cancel()
	
	// Wait for graceful shutdown with timeout
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		log.Printf("Worker %s stopped gracefully", w.config.WorkerID)
		return nil
	case <-time.After(w.config.ShutdownTimeout):
		return fmt.Errorf("worker shutdown timeout after %v", w.config.ShutdownTimeout)
	}
}

// schedulerLoop continuously polls for pending jobs
func (w *Worker) schedulerLoop() {
	defer w.wg.Done()
	
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.pollAndDispatch()
		}
	}
}

// pollAndDispatch fetches pending jobs and dispatches them for execution
func (w *Worker) pollAndDispatch() {
	w.metrics.mu.Lock()
	w.metrics.LastPollTime = time.Now()
	w.metrics.mu.Unlock()

	jobs, err := w.store.ListPending(w.config.BatchSize)
	if err != nil {
		log.Printf("Error listing pending jobs: %v", err)
		return
	}

	for _, job := range jobs {
		// Try to acquire lock
		acquired, err := w.store.AcquireLock(job.ID, w.config.WorkerID, w.config.LockTTL)
		if err != nil {
			log.Printf("Error acquiring lock for job %s: %v", job.ID, err)
			continue
		}
		
		if !acquired {
			// Another worker has this job
			continue
		}

		// Dispatch job in goroutine
		w.wg.Add(1)
		go w.executeJob(job)
	}
}

// executeJob runs a single job with retry logic
func (w *Worker) executeJob(job *Job) {
	defer w.wg.Done()
	defer w.store.ReleaseLock(job.ID, w.config.WorkerID)

	w.metrics.mu.Lock()
	w.metrics.JobsProcessed++
	w.metrics.mu.Unlock()

	// Update job status to running
	job.Status = JobStatusRunning
	job.Attempts++
	now := time.Now()
	job.StartedAt = &now
	if err := w.store.Update(job); err != nil {
		log.Printf("Error updating job %s to running: %v", job.ID, err)
		return
	}

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(w.ctx, w.config.LockTTL-5*time.Second)
	defer cancel()

	err := w.executor.Execute(execCtx, job)
	
	if err != nil {
		w.handleJobFailure(job, err)
	} else {
		w.handleJobSuccess(job)
	}
}

// handleJobSuccess marks a job as completed
func (w *Worker) handleJobSuccess(job *Job) {
	job.Status = JobStatusCompleted
	now := time.Now()
	job.CompletedAt = &now
	job.LastError = ""
	
	if err := w.store.Update(job); err != nil {
		log.Printf("Error updating job %s to completed: %v", job.ID, err)
		return
	}

	w.metrics.mu.Lock()
	w.metrics.JobsSucceeded++
	w.metrics.mu.Unlock()

	log.Printf("Job %s completed successfully", job.ID)
}

// handleJobFailure implements retry logic with dead-letter queue
func (w *Worker) handleJobFailure(job *Job, execErr error) {
	job.LastError = execErr.Error()
	
	if job.Attempts >= w.config.MaxAttempts {
		// Move to dead-letter queue
		job.Status = JobStatusDeadLetter
		now := time.Now()
		job.CompletedAt = &now
		
		w.metrics.mu.Lock()
		w.metrics.JobsDeadLettered++
		w.metrics.mu.Unlock()
		
		log.Printf("Job %s moved to dead-letter queue after %d attempts: %v", 
			job.ID, job.Attempts, execErr)
	} else {
		// Retry with exponential backoff
		job.Status = JobStatusPending
		backoff := time.Duration(job.Attempts*job.Attempts) * time.Second
		job.ScheduledAt = time.Now().Add(backoff)
		
		w.metrics.mu.Lock()
		w.metrics.JobsFailed++
		w.metrics.mu.Unlock()
		
		log.Printf("Job %s failed (attempt %d/%d), retrying in %v: %v", 
			job.ID, job.Attempts, w.config.MaxAttempts, backoff, execErr)
	}
	
	if err := w.store.Update(job); err != nil {
		log.Printf("Error updating failed job %s: %v", job.ID, err)
	}
}

func generateWorkerID() string {
	return fmt.Sprintf("worker-%d", time.Now().UnixNano())
}
