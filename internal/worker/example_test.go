package worker_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"stellarbill-backend/internal/worker"
)

// Example demonstrates basic worker usage
func Example_basicUsage() {
	// Create store and executor
	store := worker.NewMemoryStore()
	executor := worker.NewBillingExecutor()

	// Configure worker
	config := worker.DefaultConfig()
	config.PollInterval = 1 * time.Second
	config.MaxAttempts = 3

	// Create and start worker
	w := worker.NewWorker(store, executor, config)
	w.Start()

	// Schedule billing jobs
	scheduler := worker.NewScheduler(store)
	
	// Schedule immediate charge
	job1, _ := scheduler.ScheduleCharge("sub-123", time.Now(), 3)
	fmt.Printf("Scheduled charge job: %s\n", job1.ID)
	
	// Schedule future invoice
	job2, _ := scheduler.ScheduleInvoice("sub-456", time.Now().Add(1*time.Hour), 3)
	fmt.Printf("Scheduled invoice job: %s\n", job2.ID)

	// Let worker process
	time.Sleep(2 * time.Second)

	// Check metrics
	metrics := w.GetMetrics()
	fmt.Printf("Jobs processed: %d\n", metrics.JobsProcessed)

	// Graceful shutdown
	w.Stop()
}

// Example demonstrates concurrent workers
func Example_concurrentWorkers() {
	// Shared store
	store := worker.NewMemoryStore()
	executor := worker.NewBillingExecutor()

	// Start multiple workers
	config1 := worker.DefaultConfig()
	config1.WorkerID = "worker-1"
	config1.PollInterval = 1 * time.Second

	config2 := worker.DefaultConfig()
	config2.WorkerID = "worker-2"
	config2.PollInterval = 1 * time.Second

	worker1 := worker.NewWorker(store, executor, config1)
	worker2 := worker.NewWorker(store, executor, config2)

	worker1.Start()
	worker2.Start()

	// Schedule jobs
	scheduler := worker.NewScheduler(store)
	for i := 0; i < 10; i++ {
		scheduler.ScheduleCharge(fmt.Sprintf("sub-%d", i), time.Now(), 3)
	}

	// Let workers process
	time.Sleep(3 * time.Second)

	// Shutdown
	worker1.Stop()
	worker2.Stop()

	fmt.Println("All jobs processed by concurrent workers")
}

// Example demonstrates custom executor
func Example_customExecutor() {
	type CustomExecutor struct{}

	func (e *CustomExecutor) Execute(ctx context.Context, job *worker.Job) error {
		log.Printf("Custom execution for job %s", job.ID)
		// Custom billing logic here
		return nil
	}

	store := worker.NewMemoryStore()
	executor := &CustomExecutor{}
	config := worker.DefaultConfig()

	w := worker.NewWorker(store, executor, config)
	w.Start()
	defer w.Stop()

	// Schedule and process jobs
	scheduler := worker.NewScheduler(store)
	scheduler.ScheduleCharge("sub-789", time.Now(), 3)

	time.Sleep(2 * time.Second)
}
