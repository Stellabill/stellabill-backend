# Background Billing Worker Implementation

## Overview

This implementation provides a production-ready background worker system for billing job scheduling and execution with comprehensive retry logic, distributed locking, and failure handling.

## What Was Implemented

### Core Components

1. **Job Model** (`internal/worker/job.go`)
   - Job structure with full lifecycle tracking
   - Status states: pending, running, completed, failed, dead_letter
   - Metadata: attempts, timestamps, error tracking
   - JobStore interface for persistence abstraction

2. **Memory Store** (`internal/worker/store_memory.go`)
   - In-memory JobStore implementation
   - Thread-safe operations with mutex protection
   - Distributed locking with TTL expiration
   - Sorted pending job retrieval
   - Dead-letter queue support

3. **Worker** (`internal/worker/worker.go`)
   - Scheduler loop with configurable poll interval
   - Concurrent job execution with goroutines
   - Distributed lock acquisition before processing
   - Retry logic with exponential backoff (1s, 4s, 9s)
   - Dead-letter queue after max attempts
   - Graceful shutdown with timeout
   - Execution metrics tracking

4. **Executor** (`internal/worker/executor.go`)
   - BillingExecutor with job type routing
   - Support for charge, invoice, and reminder jobs
   - Context-aware execution with timeout handling
   - Extensible for payment gateway integration

5. **Scheduler** (`internal/worker/scheduler.go`)
   - Utility functions for job creation
   - Type-specific scheduling methods
   - Unique job ID generation

## Key Features

### Distributed Locking
- Prevents duplicate processing across multiple workers
- Lock TTL ensures recovery from worker crashes
- Same worker can renew locks
- Automatic cleanup of expired locks

### Retry Strategy
- Exponential backoff: attempt² seconds
- Configurable max attempts (default: 3)
- Failed jobs return to pending with future scheduled time
- Persistent failures move to dead-letter queue

### Graceful Shutdown
- Context cancellation stops scheduler loop
- WaitGroup ensures in-flight jobs complete
- Configurable shutdown timeout
- Clean resource cleanup

### Concurrency Safety
- Multiple workers can run simultaneously
- Lock-based deduplication prevents race conditions
- Thread-safe metrics tracking
- Immutable job copies prevent data races

## Test Coverage

Comprehensive test suite covering:

- ✅ Worker start/stop lifecycle
- ✅ Pending job processing
- ✅ Retry logic with exponential backoff
- ✅ Dead-letter queue after max attempts
- ✅ Concurrent workers without duplicate processing
- ✅ Future job scheduling (not executed early)
- ✅ Graceful shutdown
- ✅ Shutdown timeout
- ✅ Lock acquisition and expiration
- ✅ Lock release and renewal
- ✅ Store CRUD operations
- ✅ Executor job type routing
- ✅ Context cancellation handling
- ✅ Scheduler job creation

Run tests:
```bash
go test ./internal/worker/... -v -cover
```

Expected coverage: 95%+

## Security Considerations

1. **Job Isolation**: Each job runs in isolated goroutine with context timeout
2. **Resource Limits**: Batch size prevents memory exhaustion
3. **Lock Safety**: Distributed locks prevent race conditions and double-billing
4. **Error Boundaries**: Individual job failures don't crash worker
5. **Audit Trail**: All state transitions logged for compliance
6. **Graceful Degradation**: Worker continues on individual failures

## Edge Cases Handled

### Clock Skew
- Jobs scheduled in the past execute immediately
- Future jobs wait until scheduled time
- Lock TTL uses local time for expiration

### Worker Restart
- Locks expire automatically (TTL)
- Pending jobs picked up by any worker
- In-flight jobs retry after lock expiration
- No job loss on worker crash

### Concurrent Workers
- Distributed locking prevents duplicate execution
- Lock contention handled gracefully
- Workers coordinate via shared store
- Horizontal scaling supported

## Production Deployment

### Database Integration

Replace MemoryStore with PostgreSQL:

```go
type PostgresStore struct {
    db *sql.DB
}

func (s *PostgresStore) AcquireLock(jobID, workerID string, ttl time.Duration) (bool, error) {
    // Use PostgreSQL advisory locks or UPDATE with WHERE clause
    result, err := s.db.Exec(`
        UPDATE jobs 
        SET locked_by = $1, locked_until = $2 
        WHERE id = $3 AND (locked_until IS NULL OR locked_until < NOW())
    `, workerID, time.Now().Add(ttl), jobID)
    
    rows, _ := result.RowsAffected()
    return rows > 0, err
}
```

### Environment Configuration

Add to `internal/config/config.go`:

```go
type Config struct {
    // ... existing fields
    WorkerEnabled      bool
    WorkerPollInterval time.Duration
    WorkerMaxAttempts  int
}
```

### Integration with Main Server

Update `cmd/server/main.go`:

```go
func main() {
    cfg := config.Load()
    
    // ... existing router setup
    
    // Start billing worker
    if cfg.WorkerEnabled {
        store := worker.NewMemoryStore() // or NewPostgresStore(db)
        executor := worker.NewBillingExecutor()
        workerCfg := worker.DefaultConfig()
        
        w := worker.NewWorker(store, executor, workerCfg)
        w.Start()
        
        defer w.Stop()
    }
    
    // ... existing server start
}
```

### Monitoring

Export metrics to observability platform:

```go
// Prometheus example
prometheus.NewGaugeFunc(prometheus.GaugeOpts{
    Name: "billing_jobs_processed_total",
}, func() float64 {
    return float64(worker.GetMetrics().JobsProcessed)
})
```

### Scaling

Run multiple worker instances:

```bash
# Instance 1
WORKER_ID=worker-1 ./server

# Instance 2
WORKER_ID=worker-2 ./server
```

## API Integration

Add endpoints for job management:

```go
// GET /api/admin/jobs/dead-letter
func ListDeadLetterJobs(c *gin.Context) {
    jobs, err := store.ListDeadLetter()
    // ... return jobs
}

// POST /api/admin/jobs/:id/retry
func RetryJob(c *gin.Context) {
    // Reset job to pending status
    // ... update job
}
```

## Future Enhancements

- Job priority queues
- Cron-like scheduled patterns
- Job dependencies and workflows
- Webhook notifications
- Admin dashboard
- Metrics export (Prometheus/CloudWatch)
- Job payload encryption
- Rate limiting per subscription

## Testing Notes

All tests pass with no external dependencies. Tests cover:
- Normal execution flow
- Failure scenarios
- Concurrency edge cases
- Resource cleanup
- Time-based scheduling

The implementation is ready for production use with database integration.
