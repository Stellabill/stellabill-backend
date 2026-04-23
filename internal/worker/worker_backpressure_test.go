package worker

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// mockExecutor simulates work and tracks concurrency
type mockExecutor struct {
	active int32
	max    int32
	delay  time.Duration
}

func (m *mockExecutor) Execute(ctx context.Context, job *Job) error {
	current := atomic.AddInt32(&m.active, 1)

	// track peak concurrency
	for {
		max := atomic.LoadInt32(&m.max)
		if current > max {
			if atomic.CompareAndSwapInt32(&m.max, max, current) {
				break
			}
		} else {
			break
		}
	}

	time.Sleep(m.delay)

	atomic.AddInt32(&m.active, -1)
	return nil
}

func TestMaxConcurrencyRespected(t *testing.T) {
	store := NewMemoryStore()
	exec := &mockExecutor{delay: 50 * time.Millisecond}

	config := DefaultConfig()
	config.MaxConcurrency = 2
	config.BatchSize = 10
	config.PollInterval = 10 * time.Millisecond

	worker := NewWorker(store, exec, config)
	worker.Start()
	defer worker.Stop()

	// enqueue many jobs
	for i := 0; i < 10; i++ {
		store.Create(&Job{
			ID:          fmt.Sprintf("job-%d", i),
			Status:      JobStatusPending,
			ScheduledAt: time.Now(),
		})
	}

	time.Sleep(300 * time.Millisecond)

	if exec.max > int32(config.MaxConcurrency) {
		t.Fatalf("expected max concurrency <= %d, got %d",
			config.MaxConcurrency, exec.max)
	}
}

func TestBackpressureThrottling(t *testing.T) {
	store := NewMemoryStore()
	exec := &mockExecutor{delay: 10 * time.Millisecond}

	config := DefaultConfig()
	config.MaxQueueDepth = 2
	config.BatchSize = 10
	config.PollInterval = 10 * time.Millisecond

	worker := NewWorker(store, exec, config)
	worker.Start()
	defer worker.Stop()

	// overload queue
	for i := 0; i < 20; i++ {
		store.Create(&Job{
			ID:          fmt.Sprintf("job-%d", i),
			Status:      JobStatusPending,
			ScheduledAt: time.Now(),
		})
	}

	time.Sleep(200 * time.Millisecond)

	depth := store.QueueDepth()

	// should not drain instantly due to throttling
	if depth == 0 {
		t.Fatalf("expected throttling to slow processing, but queue drained too fast")
	}
}

func TestQueueMetrics(t *testing.T) {
	store := NewMemoryStore()

	oldJob := &Job{
		ID:          "old-job",
		Status:      JobStatusPending,
		ScheduledAt: time.Now(),
		CreatedAt:   time.Now().Add(-1 * time.Minute),
	}

	store.Create(oldJob)

	worker := NewWorker(store, &mockExecutor{}, DefaultConfig())

	metrics := worker.GetMetrics()

	if metrics.QueueDepth == 0 {
		t.Fatalf("expected queue depth > 0")
	}

	if metrics.QueueLag < 50*time.Second {
		t.Fatalf("expected lag to reflect old job, got %v", metrics.QueueLag)
	}
}

func TestGracefulShutdownDrains(t *testing.T) {
	store := NewMemoryStore()
	exec := &mockExecutor{delay: 50 * time.Millisecond}

	config := DefaultConfig()
	config.MaxConcurrency = 2
	config.ShutdownTimeout = 1 * time.Second

	worker := NewWorker(store, exec, config)
	worker.Start()

	for i := 0; i < 5; i++ {
		store.Create(&Job{
			ID:          fmt.Sprintf("job-%d", i),
			Status:      JobStatusPending,
			ScheduledAt: time.Now(),
		})
	}

	time.Sleep(50 * time.Millisecond)

	err := worker.Stop()
	if err != nil {
		t.Fatalf("expected graceful shutdown, got error: %v", err)
	}
}
