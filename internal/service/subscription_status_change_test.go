package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func TestChangeStatus_ActiveToPaused(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:         "sub-active",
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: "cust-1",
		Status:     "active",
		Amount:     "1000",
		Currency:   "USD",
		Interval:   "month",
	})
	svc := service.NewSubscriptionService(subRepo, repository.NewMockPlanRepo())

	result, err := svc.ChangeStatus(context.Background(), "tenant-1", "merchant-1", "sub-active", "paused")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Changed {
		t.Fatal("expected transition to be marked changed")
	}
	if result.PreviousStatus != "active" || result.Status != "paused" {
		t.Fatalf("unexpected transition result: %+v", result)
	}

	row, err := subRepo.FindByIDAndTenant(context.Background(), "sub-active", "tenant-1")
	if err != nil {
		t.Fatalf("expected persisted row, got %v", err)
	}
	if row.Status != "paused" {
		t.Fatalf("expected persisted status paused, got %q", row.Status)
	}
}

func TestChangeStatus_CancelledToActiveInvalid(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:       "sub-cancelled",
		TenantID: "tenant-1",
		Status:   "cancelled",
	})
	svc := service.NewSubscriptionService(subRepo, repository.NewMockPlanRepo())

	_, err := svc.ChangeStatus(context.Background(), "tenant-1", "merchant-1", "sub-cancelled", "active")
	if !errors.Is(err, service.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}

	row, _ := subRepo.FindByIDAndTenant(context.Background(), "sub-cancelled", "tenant-1")
	if row.Status != "cancelled" {
		t.Fatalf("expected status to remain cancelled, got %q", row.Status)
	}
}

func TestChangeStatus_UnknownCurrentState(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:       "sub-unknown-state",
		TenantID: "tenant-1",
		Status:   "mystery",
	})
	svc := service.NewSubscriptionService(subRepo, repository.NewMockPlanRepo())

	_, err := svc.ChangeStatus(context.Background(), "tenant-1", "merchant-1", "sub-unknown-state", "active")
	if !errors.Is(err, service.ErrUnknownCurrentState) {
		t.Fatalf("expected ErrUnknownCurrentState, got %v", err)
	}
}

func TestChangeStatus_NoOpSameStatus(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:       "sub-noop",
		TenantID: "tenant-1",
		Status:   "active",
	})
	svc := service.NewSubscriptionService(subRepo, repository.NewMockPlanRepo())

	result, err := svc.ChangeStatus(context.Background(), "tenant-1", "merchant-1", "sub-noop", "active")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Changed {
		t.Fatal("expected no-op transition to report changed=false")
	}

	row, err := subRepo.FindByIDAndTenant(context.Background(), "sub-noop", "tenant-1")
	if err != nil {
		t.Fatalf("expected persisted row, got %v", err)
	}
	if row.Status != "active" {
		t.Fatalf("expected status to remain active, got %q", row.Status)
	}
}

func TestChangeStatus_UnknownTargetStatus(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:       "sub-invalid-target",
		TenantID: "tenant-1",
		Status:   "active",
	})
	svc := service.NewSubscriptionService(subRepo, repository.NewMockPlanRepo())

	_, err := svc.ChangeStatus(context.Background(), "tenant-1", "merchant-1", "sub-invalid-target", "bogus")
	if !errors.Is(err, service.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
}

func TestChangeStatus_DeletedSubscription(t *testing.T) {
	now := time.Now()
	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:        "sub-deleted",
		TenantID:  "tenant-1",
		Status:    "active",
		DeletedAt: &now,
	})
	svc := service.NewSubscriptionService(subRepo, repository.NewMockPlanRepo())

	_, err := svc.ChangeStatus(context.Background(), "tenant-1", "merchant-1", "sub-deleted", "paused")
	if !errors.Is(err, service.ErrDeleted) {
		t.Fatalf("expected ErrDeleted, got %v", err)
	}
}

func TestChangeStatus_CrossTenantNotFound(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:       "sub-tenant",
		TenantID: "tenant-1",
		Status:   "active",
	})
	svc := service.NewSubscriptionService(subRepo, repository.NewMockPlanRepo())

	_, err := svc.ChangeStatus(context.Background(), "tenant-2", "merchant-1", "sub-tenant", "paused")
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
