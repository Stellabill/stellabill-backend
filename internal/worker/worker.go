package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"stellarbill-backend/internal/correlation"
	"stellarbill-backend/internal/security"
	"stellarbill-backend/internal/timeutil"

	"go.uber.org/zap"
)

// Config holds worker configuration
type Config struct {
	WorkerID        string
	PollInterval    time.Duration
	LockTTL         time.Duration
	MaxAttempts     int
	BatchSize       int
	ShutdownTimeout time.Duration

	// NEW: Backpressure controls
	MaxConcurrency int
	MaxQueueDepth  int
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

		// NEW defaults
		MaxConcurrency: 10,
		MaxQueueDepth:  1000,
	}
}

// JobExecutor defines the interface for executing jobs
type JobExecutor interface {
	Execute(ctx context.Context, job *Job) error
}

// Worker manages background job scheduling and execution
type Worker struct {
	config    Config
	store     JobStore
	executor  JobExecutor
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	metrics   *Metrics

	sem chan struct{}
}

// Metrics tracks worker execution statistics
type Metrics struct {
	mu               sync.RWMutex
	JobsProcessed    int64
	JobsSucceeded    int64
	JobsFailed       int64
	JobsDeadLettered int64
	LastPollTime     time.Time

	QueueDepth int
	QueueLag   time.Duration
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
		sem:      make(chan struct{}, config.MaxConcurrency), // NEW
	}
}

// GetMetrics returns a snapshot of the current worker metrics.
func (w *Worker) GetMetrics() Metrics {
	w.metrics.mu.RLock()
	defer w.metrics.mu.RUnlock()

	// NEW: add queue stats
	depth := w.store.QueueDepth()
	oldest := w.store.OldestPending()

	var queueLag time.Duration

	if oldest != nil {
		queueLag = time.Since(oldest.CreatedAt)
	}

	return Metrics{
		JobsProcessed:    w.metrics.JobsProcessed,
		JobsSucceeded:    w.metrics.JobsSucceeded,
		JobsFailed:       w.metrics.JobsFailed,
		JobsDeadLettered: w.metrics.JobsDeadLettered,
		LastPollTime:     w.metrics.LastPollTime,

		QueueDepth: depth,
		QueueLag:   queueLag,
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
	// NEW: adaptive throttling based on queue depth
	if w.store.QueueDepth() > w.config.MaxQueueDepth {
		security.ProductionLogger().Warn("Backpressure triggered: queue too deep",
			zap.Int("queue_depth", w.store.QueueDepth()))
		time.Sleep(w.config.PollInterval * 2)
		return
	}

	w.metrics.mu.Lock()
	w.metrics.LastPollTime = timeutil.NowUTC()
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

		// NEW: acquire concurrency slot (blocks if full)
		w.sem <- struct{}{}

		w.wg.Add(1)
		go func(j *Job) {
			defer func() {
				<-w.sem // release slot
				w.wg.Done()
			}()

			w.executeJob(j)
		}(job)
	}
}

// executeJob runs a single job with retry logic
func (w *Worker) executeJob(job *Job) {
	defer w.store.ReleaseLock(job.ID, w.config.WorkerID)

	w.metrics.mu.Lock()
	w.metrics.JobsProcessed++
	w.metrics.mu.Unlock()

	// Build context with job_id for correlation
	baseCtx := correlation.WithJobID(w.ctx, job.ID)

	// Create root OTel span for this job
	spanOpts := []trace.SpanStartOption{
		trace.WithAttributes(
			attribute.String("job.id", job.ID),
			attribute.String("job.type", job.Type),
			attribute.String("job.subscription_id", job.SubscriptionID),
			attribute.Int("job.attempt", job.Attempts),
		),
		trace.WithSpanKind(trace.SpanKindConsumer),
	}

	// Link to parent HTTP trace if job originated from an HTTP request
	if job.ParentTraceID != "" {
		traceID, err := trace.TraceIDFromHex(job.ParentTraceID)
		if err == nil {
			link := trace.Link{
				SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
					TraceID:    traceID,
					TraceFlags: trace.FlagsSampled,
				}),
			}
			spanOpts = append(spanOpts, trace.WithLinks(link))
		}
	}

	tracer := otel.Tracer("worker")
	spanCtx, span := tracer.Start(baseCtx, "worker.executeJob", spanOpts...)
	defer span.End()

	// Update job status to running
	job.Status = JobStatusRunning
	job.Attempts++
	now := timeutil.NowUTC()
	job.StartedAt = &now
	if err := w.store.Update(job); err != nil {
		security.ProductionLogger().Error("Error updating job to running",
			zap.String("job_id", job.ID),
			zap.Error(err))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	// Execute with timeout using span context
	execCtx, cancel := context.WithTimeout(spanCtx, w.config.LockTTL-5*time.Second)
	defer cancel()

	err := w.executor.Execute(execCtx, job)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		w.handleJobFailure(job, err)
	} else {
		span.SetStatus(codes.Ok, "")
		w.handleJobSuccess(job)
	}
}

// handleJobSuccess marks a job as completed
func (w *Worker) handleJobSuccess(job *Job) {
	job.Status = JobStatusCompleted
	now := timeutil.NowUTC()
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
		zap.String("job_id", job.ID))
}

// handleJobFailure implements retry logic with dead-letter queue
func (w *Worker) handleJobFailure(job *Job, execErr error) {
	job.LastError = execErr.Error()

	if job.Attempts >= w.config.MaxAttempts {
		// Move to dead-letter queue
		job.Status = JobStatusDeadLetter
		now := timeutil.NowUTC()
		job.CompletedAt = &now

		w.metrics.mu.Lock()
		w.metrics.JobsDeadLettered++
		w.metrics.mu.Unlock()

		security.ProductionLogger().Warn("Job moved to dead-letter queue",
			zap.String("job_id", job.ID),
			zap.Int("attempts", job.Attempts),
			zap.Error(execErr))
	} else {
		// Retry with exponential backoff
		job.Status = JobStatusPending
		backoff := time.Duration(job.Attempts*job.Attempts) * time.Second
		job.ScheduledAt = timeutil.NowUTC().Add(backoff)

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
	return fmt.Sprintf("worker-%d", timeutil.NowUTC().UnixNano())
}
