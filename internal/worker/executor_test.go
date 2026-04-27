package worker

import (
	"context"
	"testing"
	"time"
)

func TestBillingExecutor_ExecuteCharge(t *testing.T) {
	executor := NewBillingExecutor()

	job := &Job{
		ID:             "charge-1",
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusRunning,
		ScheduledAt:    time.Now(),
		MaxAttempts:    3,
	}

	ctx := context.Background()
	err := executor.Execute(ctx, job)

	if err != nil {
		t.Errorf("Expected successful charge execution, got error: %v", err)
	}
}

func TestBillingExecutor_ExecuteInvoice(t *testing.T) {
	executor := NewBillingExecutor()

	job := &Job{
		ID:             "invoice-1",
		SubscriptionID: "sub-1",
		Type:           "invoice",
		Status:         JobStatusRunning,
		ScheduledAt:    time.Now(),
		MaxAttempts:    3,
	}

	ctx := context.Background()
	err := executor.Execute(ctx, job)

	if err != nil {
		t.Errorf("Expected successful invoice execution, got error: %v", err)
	}
}

func TestBillingExecutor_ExecuteReminder(t *testing.T) {
	executor := NewBillingExecutor()

	job := &Job{
		ID:             "reminder-1",
		SubscriptionID: "sub-1",
		Type:           "reminder",
		Status:         JobStatusRunning,
		ScheduledAt:    time.Now(),
		MaxAttempts:    3,
	}

	ctx := context.Background()
	err := executor.Execute(ctx, job)

	if err != nil {
		t.Errorf("Expected successful reminder execution, got error: %v", err)
	}
}

func TestBillingExecutor_UnknownJobType(t *testing.T) {
	executor := NewBillingExecutor()

	job := &Job{
		ID:             "unknown-1",
		SubscriptionID: "sub-1",
		Type:           "unknown-type",
		Status:         JobStatusRunning,
		ScheduledAt:    time.Now(),
		MaxAttempts:    3,
	}

	ctx := context.Background()
	err := executor.Execute(ctx, job)

	if err == nil {
		t.Error("Expected error for unknown job type, got nil")
	}
}

func TestBillingExecutor_ContextCancellation(t *testing.T) {
	executor := NewBillingExecutor()

	job := &Job{
		ID:             "cancel-1",
		SubscriptionID: "sub-1",
		Type:           "charge",
		Status:         JobStatusRunning,
		ScheduledAt:    time.Now(),
		MaxAttempts:    3,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := executor.Execute(ctx, job)

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}
}

