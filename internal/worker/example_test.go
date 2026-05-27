package worker

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"stellarbill-backend/internal/security"
	"time"
)

type CustomExecutor struct{}

func (e *CustomExecutor) Execute(ctx context.Context, job *Job) error {
	security.ProductionLogger().Info("Custom execution for job",
		zap.String("job_id", job.ID))
	// Custom billing logic here
	return nil
}

func ExampleCustomExecutor() {
	// Create store and executor
	store := NewMemoryStore()
	executor := &CustomExecutor{}
	config := DefaultConfig()
	
	// Create and start worker
	w := NewWorker(store, executor, config)
	w.Start()
	
	// Schedule billing jobs
	scheduler := NewScheduler(store)
	scheduler.ScheduleCharge("sub-123", time.Now(), 3)
	
	// Stop worker
	w.Stop()
}

func ExampleWorker() {
	// Shared store
	store := NewMemoryStore()
	executor := NewBillingExecutor()
	
	config1 := DefaultConfig()
	config1.WorkerID = "worker-1"
	config2 := DefaultConfig()
	config2.WorkerID = "worker-2"
	
	worker1 := NewWorker(store, executor, config1)
	worker2 := NewWorker(store, executor, config2)
	
	worker1.Start()
	worker2.Start()
	
	// Schedule jobs
	scheduler := NewScheduler(store)
	for i := 0; i < 10; i++ {
		scheduler.ScheduleCharge("sub-"+fmt.Sprint(i), time.Now(), 3)
	}
	
	// Stop workers
	worker1.Stop()
	worker2.Stop()
}

