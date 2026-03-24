# Quick Start: Billing Worker

## 30-Second Overview

The billing worker is a background job scheduler that:
- Processes billing operations (charges, invoices, reminders)
- Retries failed jobs automatically (1s, 4s, 9s backoff)
- Prevents duplicate processing with distributed locks
- Moves persistent failures to dead-letter queue

## Run Tests

```bash
go test ./internal/worker/... -v -cover
```

Expected: All tests pass, 95%+ coverage

## Basic Usage

```go
package main

import (
    "time"
    "stellarbill-backend/internal/worker"
)

func main() {
    // Setup
    store := worker.NewMemoryStore()
    executor := worker.NewBillingExecutor()
    config := worker.DefaultConfig()
    
    // Start worker
    w := worker.NewWorker(store, executor, config)
    w.Start()
    defer w.Stop()
    
    // Schedule a billing job
    scheduler := worker.NewScheduler(store)
    job, _ := scheduler.ScheduleCharge("sub-123", time.Now(), 3)
    
    // Job will be processed automatically
    // Check metrics
    metrics := w.GetMetrics()
    println("Processed:", metrics.JobsProcessed)
}
```

## Key Files

- `internal/worker/README.md` - Full documentation
- `internal/worker/INTEGRATION.md` - Integration guide
- `internal/worker/SECURITY.md` - Security analysis

## Architecture

```
Job Lifecycle:
Pending → Running → Completed
    ↓         ↓
    └─────→ Failed → Retry (exponential backoff)
              ↓
         Dead Letter (after max attempts)

Components:
- Job: Task definition with metadata
- JobStore: Persistence layer (in-memory or database)
- Worker: Scheduler loop and execution coordinator
- Executor: Billing operation implementation
- Scheduler: Job creation utilities
```

## Configuration

```go
config := worker.Config{
    WorkerID:        "worker-1",
    PollInterval:    5 * time.Second,  // How often to check for jobs
    LockTTL:         30 * time.Second, // Lock expiration time
    MaxAttempts:     3,                // Retries before dead-letter
    BatchSize:       10,               // Jobs per poll
    ShutdownTimeout: 30 * time.Second, // Graceful shutdown timeout
}
```

## Job Types

- **charge**: Process subscription payment
- **invoice**: Generate and send invoice
- **reminder**: Send payment reminder

## Monitoring

```go
metrics := worker.GetMetrics()
// JobsProcessed, JobsSucceeded, JobsFailed, JobsDeadLettered, LastPollTime
```

## Production Checklist

- [ ] Replace MemoryStore with PostgresStore
- [ ] Configure environment variables
- [ ] Set up monitoring and alerting
- [ ] Review security considerations
- [ ] Test with real payment gateway
- [ ] Configure multiple worker instances
- [ ] Set up dead-letter queue monitoring

## Common Patterns

### Schedule Immediate Job

```go
scheduler.ScheduleCharge("sub-123", time.Now(), 3)
```

### Schedule Future Job

```go
nextBilling := time.Now().Add(30 * 24 * time.Hour)
scheduler.ScheduleCharge("sub-123", nextBilling, 3)
```

### Check Job Status

```go
job, err := store.Get("job-id")
if err != nil {
    // Handle error
}
fmt.Println("Status:", job.Status)
fmt.Println("Attempts:", job.Attempts)
```

### List Failed Jobs

```go
deadLetters, err := store.ListDeadLetter()
for _, job := range deadLetters {
    fmt.Printf("Job %s failed: %s\n", job.ID, job.LastError)
}
```

## Troubleshooting

### Worker Not Processing Jobs

- Check job ScheduledAt is in the past
- Verify worker is running (check logs)
- Check lock status (may be held by another worker)

### Jobs Failing Repeatedly

- Check executor implementation
- Review job payload
- Verify external dependencies (payment gateway)
- Check logs for error details

### Duplicate Processing

- Verify distributed locking is working
- Check lock TTL configuration
- Ensure unique worker IDs
- Review concurrent worker setup

## Need Help?

1. Read `internal/worker/README.md` for detailed docs
2. Check `internal/worker/INTEGRATION.md` for integration examples
3. Review `internal/worker/SECURITY.md` for security guidance
4. Look at test files for usage examples
