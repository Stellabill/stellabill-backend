# Billing Worker Package

Background worker system for billing job scheduling and execution with retry logic and failure handling.

## Features

- **Job Scheduling**: Schedule billing jobs (charges, invoices, reminders) with configurable execution times
- **Distributed Locking**: Prevents duplicate processing when running multiple worker instances
- **Retry Policy**: Automatic retry with exponential backoff for failed jobs
- **Dead-Letter Queue**: Failed jobs after max attempts are moved to dead-letter for manual review
- **Graceful Shutdown**: Workers complete in-flight jobs before shutting down
- **Metrics**: Track job processing statistics (processed, succeeded, failed, dead-lettered)
- **Concurrent Workers**: Multiple workers can run safely without duplicate processing

## Architecture

### Components

1. **Job**: Represents a billing task with metadata (ID, type, status, attempts, etc.)
2. **JobStore**: Interface for job persistence (in-memory implementation provided)
3. **JobExecutor**: Interface for executing billing operations
4. **Worker**: Manages the scheduling loop and job execution
5. **Scheduler**: Utility for creating and scheduling jobs

### Job Lifecycle

```
Pending → Running → Completed
    ↓         ↓
    └─────→ Failed → Retry (with backoff)
              ↓
         Dead Letter (after max attempts)
```

## Usage

### Basic Setup

```go
import "stellarbill-backend/internal/worker"

// Create store and executor
store := worker.NewMemoryStore()
executor := worker.NewBillingExecutor()

// Configure worker
config := worker.DefaultConfig()
config.PollInterval = 5 * time.Second
config.MaxAttempts = 3

// Create and start worker
w := worker.NewWorker(store, executor, config)
w.Start()

// Schedule a billing job
scheduler := worker.NewScheduler(store)
job, err := scheduler.ScheduleCharge("sub-123", time.Now(), 3)

// Graceful shutdown
w.Stop()
```

### Configuration Options

```go
type Config struct {
    WorkerID        string        // Unique worker identifier
    PollInterval    time.Duration // How often to check for pending jobs
    LockTTL         time.Duration // Lock expiration time
    MaxAttempts     int           // Max retry attempts before dead-letter
    BatchSize       int           // Max jobs to process per poll
    ShutdownTimeout time.Duration // Max time to wait for graceful shutdown
}
```

### Job Types

- **charge**: Process subscription payment
- **invoice**: Generate and send invoice
- **reminder**: Send payment reminder notification

### Monitoring

```go
metrics := worker.GetMetrics()
fmt.Printf("Processed: %d\n", metrics.JobsProcessed)
fmt.Printf("Succeeded: %d\n", metrics.JobsSucceeded)
fmt.Printf("Failed: %d\n", metrics.JobsFailed)
fmt.Printf("Dead-lettered: %d\n", metrics.JobsDeadLettered)
```

## Concurrency & Locking

The worker uses distributed locking to prevent duplicate processing:

1. Worker polls for pending jobs
2. Attempts to acquire lock for each job
3. Only processes jobs it successfully locks
4. Releases lock after completion or failure
5. Locks expire automatically (TTL) to handle worker crashes

Multiple workers can run concurrently without conflicts.

## Retry Strategy

Failed jobs are retried with exponential backoff:

- Attempt 1 fails → retry in 1 second
- Attempt 2 fails → retry in 4 seconds
- Attempt 3 fails → retry in 9 seconds
- After max attempts → move to dead-letter queue

## Error Handling

- **Temporary failures**: Job retried with backoff
- **Persistent failures**: Job moved to dead-letter after max attempts
- **Context cancellation**: Job execution respects context timeouts
- **Worker crashes**: Locks expire, allowing other workers to pick up jobs

## Testing

Run tests with coverage:

```bash
go test ./internal/worker/... -v -cover
```

Test coverage includes:
- Job creation and lifecycle
- Concurrent worker scenarios
- Lock acquisition and expiration
- Retry logic and dead-letter handling
- Graceful shutdown
- Clock skew scenarios

## Security Considerations

1. **Job Isolation**: Each job runs in isolated goroutine with timeout
2. **Lock Safety**: Distributed locks prevent race conditions
3. **Graceful Degradation**: Worker continues on individual job failures
4. **Resource Limits**: Batch size limits prevent resource exhaustion
5. **Audit Trail**: All job state changes are logged

## Production Deployment

### Database Integration

Replace `MemoryStore` with a persistent implementation:

```go
type PostgresStore struct {
    db *sql.DB
}

func (s *PostgresStore) Create(job *Job) error {
    // INSERT INTO jobs ...
}

// Implement other JobStore methods
```

### Multiple Workers

Run multiple worker instances for high availability:

```bash
# Worker 1
./worker --worker-id=worker-1 --poll-interval=5s

# Worker 2
./worker --worker-id=worker-2 --poll-interval=5s
```

### Monitoring

Integrate with monitoring systems:

```go
// Export metrics to Prometheus, CloudWatch, etc.
prometheus.NewGaugeFunc(prometheus.GaugeOpts{
    Name: "billing_jobs_processed_total",
}, func() float64 {
    return float64(worker.GetMetrics().JobsProcessed)
})
```

## Future Enhancements

- [ ] PostgreSQL store implementation
- [ ] Job priority queues
- [ ] Scheduled job patterns (cron-like)
- [ ] Job dependencies and workflows
- [ ] Webhook notifications for job events
- [ ] Admin API for job management
- [ ] Prometheus metrics exporter
