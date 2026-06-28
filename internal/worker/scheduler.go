package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/security"
	"stellarbill-backend/internal/timeutil"
)

// Scheduler provides utilities for creating and scheduling billing jobs
type Scheduler struct {
	store JobStore
}

// NewScheduler creates a new job scheduler
func NewScheduler(store JobStore) *Scheduler {
	return &Scheduler{store: store}
}

// ScheduleCharge creates a charge job for a subscription
func (s *Scheduler) ScheduleCharge(subscriptionID string, scheduledAt time.Time, maxAttempts int) (*Job, error) {
	job := &Job{
		ID:             generateJobID("charge"),
		SubscriptionID: subscriptionID,
		Type:           "charge",
		Status:         JobStatusPending,
		ScheduledAt:    timeutil.NormalizeUTC(scheduledAt),
		MaxAttempts:    maxAttempts,
		Attempts:       0,
	}

	if err := s.store.Create(job); err != nil {
		return nil, fmt.Errorf("failed to schedule charge: %w", err)
	}

	return job, nil
}

// ScheduleInvoice creates an invoice generation job
func (s *Scheduler) ScheduleInvoice(subscriptionID string, scheduledAt time.Time, maxAttempts int) (*Job, error) {
	job := &Job{
		ID:             generateJobID("invoice"),
		SubscriptionID: subscriptionID,
		Type:           "invoice",
		Status:         JobStatusPending,
		ScheduledAt:    timeutil.NormalizeUTC(scheduledAt),
		MaxAttempts:    maxAttempts,
		Attempts:       0,
	}

	if err := s.store.Create(job); err != nil {
		return nil, fmt.Errorf("failed to schedule invoice: %w", err)
	}

	return job, nil
}

// ScheduleReminder creates a payment reminder job
func (s *Scheduler) ScheduleReminder(subscriptionID string, scheduledAt time.Time, maxAttempts int) (*Job, error) {
	job := &Job{
		ID:             generateJobID("reminder"),
		SubscriptionID: subscriptionID,
		Type:           "reminder",
		Status:         JobStatusPending,
		ScheduledAt:    timeutil.NormalizeUTC(scheduledAt),
		MaxAttempts:    maxAttempts,
		Attempts:       0,
	}

	if err := s.store.Create(job); err != nil {
		return nil, fmt.Errorf("failed to schedule reminder: %w", err)
	}

	return job, nil
}

func generateJobID(jobType string) string {
	return fmt.Sprintf("%s-%d", jobType, timeutil.NowUTC().UnixNano())
}

// IdempotencyCleanupJob periodically cleans up expired idempotency keys.
type IdempotencyCleanupJob struct {
	store middleware.IdempotencyStore
}

// NewIdempotencyCleanupJob creates a new IdempotencyCleanupJob.
func NewIdempotencyCleanupJob(store middleware.IdempotencyStore) *IdempotencyCleanupJob {
	return &IdempotencyCleanupJob{store: store}
}

// Start begins the 15-minute scheduled ticker for the job.
func (j *IdempotencyCleanupJob) Start(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	// Run once immediately (optional, remove if strictly only on tick)
	j.Run(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.Run(ctx)
		}
	}
}

// Run executes the cleanup logic with a 5-minute budget.
func (j *IdempotencyCleanupJob) Run(baseCtx context.Context) {
	ctx, cancel := context.WithTimeout(baseCtx, 5*time.Minute)
	defer cancel()

	// Update the pending metric
	pending, err := j.store.CountExpiredPending(ctx)
	if err != nil {
		log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to count expired idempotency keys: %v", err)))
	} else {
		middleware.IdempotencyKeysExpiredPending.Set(float64(pending))
	}

	// Loop calling DeleteExpiredBatch(ctx, 5000)
	for {
		// Break cleanly if we exceed the 5-minute execution budget
		if ctx.Err() != nil {
			log.Printf("%s", security.MaskPII(fmt.Sprintf("Idempotency cleanup stopped: %v", ctx.Err())))
			break
		}

		deleted, err := j.store.DeleteExpiredBatch(ctx, 5000)
		if err != nil {
			log.Printf("%s", security.MaskPII(fmt.Sprintf("Error deleting expired idempotency batch: %v", err)))
			break
		}

		if deleted > 0 {
			middleware.IdempotencyKeysPurgedTotal.Add(float64(deleted))
		}

		// Completion: The loop should break if DeleteExpiredBatch returns 0 rows deleted.
		if deleted == 0 {
			break
		}

		// Throttle database load between batches
		time.Sleep(50 * time.Millisecond)
	}
}
