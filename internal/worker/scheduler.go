package worker

import (
	"fmt"
	"time"
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
		ScheduledAt:    scheduledAt,
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
		ScheduledAt:    scheduledAt,
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
		ScheduledAt:    scheduledAt,
		MaxAttempts:    maxAttempts,
		Attempts:       0,
	}

	if err := s.store.Create(job); err != nil {
		return nil, fmt.Errorf("failed to schedule reminder: %w", err)
	}

	return job, nil
}

func generateJobID(jobType string) string {
	return fmt.Sprintf("%s-%d", jobType, time.Now().UnixNano())
}

