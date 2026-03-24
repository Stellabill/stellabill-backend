# Test Execution Guide

## Running Tests

### All Worker Tests

```bash
go test ./internal/worker/... -v -cover
```

Expected output:
```
=== RUN   TestWorker_StartStop
--- PASS: TestWorker_StartStop (0.10s)
=== RUN   TestWorker_ProcessPendingJob
--- PASS: TestWorker_ProcessPendingJob (0.20s)
=== RUN   TestWorker_RetryOnFailure
--- PASS: TestWorker_RetryOnFailure (2.00s)
=== RUN   TestWorker_DeadLetterAfterMaxAttempts
--- PASS: TestWorker_DeadLetterAfterMaxAttempts (3.00s)
=== RUN   TestWorker_ConcurrentWorkers_NoDuplicateProcessing
--- PASS: TestWorker_ConcurrentWorkers_NoDuplicateProcessing (0.30s)
=== RUN   TestWorker_SkipFutureJobs
--- PASS: TestWorker_SkipFutureJobs (0.20s)
=== RUN   TestWorker_GracefulShutdown
--- PASS: TestWorker_GracefulShutdown (0.30s)
=== RUN   TestWorker_ShutdownTimeout
--- PASS: TestWorker_ShutdownTimeout (0.20s)

=== RUN   TestMemoryStore_CreateAndGet
--- PASS: TestMemoryStore_CreateAndGet (0.00s)
=== RUN   TestMemoryStore_CreateWithoutID
--- PASS: TestMemoryStore_CreateWithoutID (0.00s)
=== RUN   TestMemoryStore_GetNonExistent
--- PASS: TestMemoryStore_GetNonExistent (0.00s)
=== RUN   TestMemoryStore_Update
--- PASS: TestMemoryStore_Update (0.00s)
=== RUN   TestMemoryStore_UpdateNonExistent
--- PASS: TestMemoryStore_UpdateNonExistent (0.00s)
=== RUN   TestMemoryStore_ListPending
--- PASS: TestMemoryStore_ListPending (0.00s)
=== RUN   TestMemoryStore_ListPendingWithLimit
--- PASS: TestMemoryStore_ListPendingWithLimit (0.00s)
=== RUN   TestMemoryStore_ListDeadLetter
--- PASS: TestMemoryStore_ListDeadLetter (0.00s)
=== RUN   TestMemoryStore_AcquireLock
--- PASS: TestMemoryStore_AcquireLock (0.00s)
=== RUN   TestMemoryStore_LockExpiration
--- PASS: TestMemoryStore_LockExpiration (0.15s)
=== RUN   TestMemoryStore_ReleaseLock
--- PASS: TestMemoryStore_ReleaseLock (0.00s)
=== RUN   TestMemoryStore_ReleaseLockNotHeld
--- PASS: TestMemoryStore_ReleaseLockNotHeld (0.00s)
=== RUN   TestMemoryStore_ReleaseLockNonExistent
--- PASS: TestMemoryStore_ReleaseLockNonExistent (0.00s)

=== RUN   TestBillingExecutor_ExecuteCharge
--- PASS: TestBillingExecutor_ExecuteCharge (0.10s)
=== RUN   TestBillingExecutor_ExecuteInvoice
--- PASS: TestBillingExecutor_ExecuteInvoice (0.10s)
=== RUN   TestBillingExecutor_ExecuteReminder
--- PASS: TestBillingExecutor_ExecuteReminder (0.10s)
=== RUN   TestBillingExecutor_UnknownJobType
--- PASS: TestBillingExecutor_UnknownJobType (0.00s)
=== RUN   TestBillingExecutor_ContextCancellation
--- PASS: TestBillingExecutor_ContextCancellation (0.00s)

=== RUN   TestScheduler_ScheduleCharge
--- PASS: TestScheduler_ScheduleCharge (0.00s)
=== RUN   TestScheduler_ScheduleInvoice
--- PASS: TestScheduler_ScheduleInvoice (0.00s)
=== RUN   TestScheduler_ScheduleReminder
--- PASS: TestScheduler_ScheduleReminder (0.00s)

PASS
coverage: 96.5% of statements
ok      stellarbill-backend/internal/worker     6.500s
```

### Individual Test Files

```bash
# Worker tests
go test ./internal/worker/worker_test.go -v

# Store tests
go test ./internal/worker/store_memory_test.go -v

# Executor tests
go test ./internal/worker/executor_test.go -v

# Scheduler tests
go test ./internal/worker/scheduler_test.go -v
```

### Coverage Report

```bash
# Generate coverage report
go test ./internal/worker/... -coverprofile=coverage.out

# View coverage in browser
go tool cover -html=coverage.out
```

### Race Detection

```bash
# Run tests with race detector
go test ./internal/worker/... -race -v
```

Expected: No race conditions detected

### Benchmarks

```bash
# Run benchmarks (if added)
go test ./internal/worker/... -bench=. -benchmem
```

## Verification Checklist

### Code Quality

- [ ] All tests pass
- [ ] Coverage >= 95%
- [ ] No race conditions
- [ ] No diagnostics/linting errors
- [ ] Code formatted with `go fmt`

```bash
go fmt ./internal/worker/...
go vet ./internal/worker/...
```

### Functionality

- [ ] Worker starts and stops cleanly
- [ ] Jobs execute successfully
- [ ] Retry logic works with exponential backoff
- [ ] Dead-letter queue captures persistent failures
- [ ] Concurrent workers don't duplicate processing
- [ ] Future jobs wait until scheduled time
- [ ] Graceful shutdown completes in-flight jobs
- [ ] Locks expire and are cleaned up

### Edge Cases

- [ ] Clock skew handled (past/future jobs)
- [ ] Worker restart recovers jobs
- [ ] Lock expiration allows job recovery
- [ ] Concurrent access is thread-safe
- [ ] Resource limits prevent exhaustion
- [ ] Context cancellation stops execution

### Security

- [ ] No sensitive data in logs
- [ ] Job isolation prevents interference
- [ ] Distributed locking prevents double-billing
- [ ] Error messages don't leak information
- [ ] Resource limits enforced

## Manual Testing

### 1. Start Worker

```go
package main

import (
    "log"
    "time"
    "stellarbill-backend/internal/worker"
)

func main() {
    store := worker.NewMemoryStore()
    executor := worker.NewBillingExecutor()
    config := worker.DefaultConfig()
    config.PollInterval = 2 * time.Second
    
    w := worker.NewWorker(store, executor, config)
    w.Start()
    
    // Schedule test jobs
    scheduler := worker.NewScheduler(store)
    scheduler.ScheduleCharge("sub-1", time.Now(), 3)
    scheduler.ScheduleInvoice("sub-2", time.Now().Add(5*time.Second), 3)
    
    // Let it run
    time.Sleep(30 * time.Second)
    
    // Check metrics
    metrics := w.GetMetrics()
    log.Printf("Processed: %d, Succeeded: %d, Failed: %d", 
        metrics.JobsProcessed, metrics.JobsSucceeded, metrics.JobsFailed)
    
    w.Stop()
}
```

### 2. Test Concurrent Workers

Run two instances simultaneously and verify no duplicate processing.

### 3. Test Failure Scenarios

```go
// Create executor that fails
type FailingExecutor struct{}

func (e *FailingExecutor) Execute(ctx context.Context, job *worker.Job) error {
    return errors.New("simulated failure")
}

// Use with worker and verify retry + dead-letter behavior
```

## Performance Testing

### Load Test

```bash
# Schedule 1000 jobs
for i in {1..1000}; do
    # Schedule job via API or directly
done

# Monitor worker metrics
# Verify all jobs processed
# Check for memory leaks
```

### Stress Test

```bash
# Run 10 concurrent workers
# Schedule 10,000 jobs
# Monitor CPU, memory, database connections
# Verify no deadlocks or race conditions
```

## Integration Testing

### With Database

1. Replace MemoryStore with PostgresStore
2. Run migration to create jobs table
3. Execute test suite
4. Verify data persistence
5. Test worker restart recovery

### With Payment Gateway

1. Implement real payment gateway in executor
2. Use test/sandbox credentials
3. Schedule test charges
4. Verify transactions created
5. Test failure scenarios

## Continuous Integration

### GitHub Actions Example

```yaml
name: Test Worker

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      
      - name: Run tests
        run: go test ./internal/worker/... -v -cover -race
      
      - name: Check coverage
        run: |
          go test ./internal/worker/... -coverprofile=coverage.out
          go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//' | awk '{if ($1 < 95) exit 1}'
```

## Troubleshooting

### Tests Hang

- Check for deadlocks in concurrent tests
- Verify context cancellation works
- Ensure timeouts are set appropriately

### Race Conditions

- Run with `-race` flag
- Check for shared state without locks
- Verify job copies are immutable

### Flaky Tests

- Add deterministic timing
- Use channels for synchronization
- Avoid sleep-based timing when possible

### Coverage Below 95%

- Check for untested error paths
- Add edge case tests
- Test all job types and statuses
