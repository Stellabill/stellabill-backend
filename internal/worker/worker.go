package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"stellarbill-backend/internal/requestid"
	"stellarbill-backend/internal/security"
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

// GetMetrics returns a snapshot of the current worker metrics.
func (w *Worker) GetMetrics() Metrics {
	w.metrics.mu.RLock()
	defer w.metrics.mu.RUnlock()

	return Metrics{
		JobsProcessed:    w.metrics.JobsProcessed,
		JobsSucceeded:    w.metrics.JobsSucceeded,
		JobsFailed:       w.metrics.JobsFailed,
		JobsDeadLettered: w.metrics.JobsDeadLettered,
		LastPollTime:     w.metrics.LastPollTime,
	}
}

// Start begins the worker's scheduling loop
func (w *Worker) Start() {
	w.wg.Add(1)
	go w.schedulerLoop()
	security.ProductionLogger().Info("Worker started",
		zap.String("worker_id", w.config.WorkerID),
		zap.Duration("poll_interval", w.config.PollInterval))
}

// Stop gracefully shuts down the worker
func (w *Worker) Stop() error {
	security.ProductionLogger().Info("Worker shutting down",
		zap.String("worker_id", w.config.WorkerID))
	w.cancel()

	// Wait for graceful shutdown with timeout
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		security.ProductionLogger().Info("Worker stopped gracefully",
			zap.String("worker_id", w.config.WorkerID))
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
		security.ProductionLogger().Error("Error listing pending jobs",
			zap.Error(err))
		return
	}

	for _, job := range jobs {
		// Try to acquire lock
		acquired, err := w.store.AcquireLock(job.ID, w.config.WorkerID, w.config.LockTTL)
		if err != nil {
			security.ProductionLogger().Error("Error acquiring lock",
				zap.String("job_id", job.ID),
				zap.Error(err))
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
		security.ProductionLogger().Error("Error updating job to running",
			zap.String("job_id", job.ID),
			zap.Error(err))
		return
	}

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(w.ctx, w.config.LockTTL-5*time.Second)
	defer cancel()

	// Inject job_id and worker_id into execution context
	execCtx = requestid.WithJobID(execCtx, job.ID)
	execCtx = requestid.WithWorkerID(execCtx, w.config.WorkerID)

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
		security.ProductionLogger().Error("Error updating job to completed",
			zap.String("job_id", job.ID),
			zap.Error(err))
		return
	}

	w.metrics.mu.Lock()
	w.metrics.JobsSucceeded++
	w.metrics.mu.Unlock()

	security.ProductionLogger().Info("Job completed successfully",
		zap.String("job_id", job.ID),
		zap.String("worker_id", w.config.WorkerID))
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
		
		security.ProductionLogger().Warn("Job moved to dead-letter queue",
			zap.String("job_id", job.ID),
			zap.String("worker_id", w.config.WorkerID),
			zap.Int("attempts", job.Attempts),
			zap.Error(execErr))
	} else {
		// Retry with exponential backoff
		job.Status = JobStatusPending
		backoff := time.Duration(job.Attempts*job.Attempts) * time.Second
		job.ScheduledAt = time.Now().Add(backoff)

		w.metrics.mu.Lock()
		w.metrics.JobsFailed++
		w.metrics.mu.Unlock()
		
		security.ProductionLogger().Warn("Job failed, retrying",
			zap.String("job_id", job.ID),
			zap.Int("attempt", job.Attempts),
			zap.Int("max_attempts", w.config.MaxAttempts),
			zap.Duration("backoff", backoff),
			zap.Error(execErr))
	}

	if err := w.store.Update(job); err != nil {
		security.ProductionLogger().Error("Error updating failed job",
			zap.String("job_id", job.ID),
			zap.Error(err))
	}
}

func generateWorkerID() string {
	return fmt.Sprintf("worker-%d", time.Now().UnixNano())
}

