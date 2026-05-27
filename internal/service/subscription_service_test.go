package service_test

import (
	"context"
	"testing"
	"time"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func TestGetDetail_HappyPath(t *testing.T) {
	plan := &repository.PlanRow{
		ID:          "plan-1",
		Name:        "Pro",
		Amount:      "2999",
		Currency:    "usd",
		Interval:    "month",
		Description: "Pro plan",
	}
	sub := &repository.SubscriptionRow{
		ID:          "sub-1",
		PlanID:      "plan-1",
		TenantID:    "tenant-1",
		CustomerID:  "cust-1",
		Status:      "active",
		Amount:      "2999",
		Currency:    "usd",
		Interval:    "month",
		NextBilling: "2024-08-01T00:00:00Z",
		DeletedAt:   nil,
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(sub),
		repository.NewMockPlanRepo(plan),
	)

	detail, warnings, err := svc.GetDetail(context.Background(), "tenant-1", "cust-1", "sub-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}

	// Core fields
	if detail.ID != "sub-1" {
		t.Errorf("ID: got %q, want %q", detail.ID, "sub-1")
	}
	if detail.PlanID != "plan-1" {
		t.Errorf("PlanID: got %q, want %q", detail.PlanID, "plan-1")
	}
	if detail.Customer != "cust-1" {
		t.Errorf("Customer: got %q, want %q", detail.Customer, "cust-1")
	}
	if detail.Status != "active" {
		t.Errorf("Status: got %q, want %q", detail.Status, "active")
	}
	if detail.Interval != "month" {
		t.Errorf("Interval: got %q, want %q", detail.Interval, "month")
	}

	// Plan metadata
	if detail.Plan == nil {
		t.Fatal("expected Plan to be non-nil")
	}
	if detail.Plan.PlanID != "plan-1" {
		t.Errorf("Plan.PlanID: got %q, want %q", detail.Plan.PlanID, "plan-1")
	}
	if detail.Plan.Name != "Pro" {
		t.Errorf("Plan.Name: got %q, want %q", detail.Plan.Name, "Pro")
	}
	if detail.Plan.Currency != "usd" {
		t.Errorf("Plan.Currency: got %q, want %q", detail.Plan.Currency, "usd")
	}

	// Billing summary
	if detail.BillingSummary.AmountCents != 2999 {
		t.Errorf("AmountCents: got %d, want 2999", detail.BillingSummary.AmountCents)
	}
	if detail.BillingSummary.Currency != "USD" {
		t.Errorf("Currency: got %q, want %q", detail.BillingSummary.Currency, "USD")
	}
	if detail.BillingSummary.NextBillingDate == nil {
		t.Error("expected NextBillingDate to be non-nil")
	} else if *detail.BillingSummary.NextBillingDate != "2024-08-01T00:00:00Z" {
		t.Errorf("NextBillingDate: got %q, want %q", *detail.BillingSummary.NextBillingDate, "2024-08-01T00:00:00Z")
	}
}

func TestGetDetail_MissingPlan(t *testing.T) {
	sub := &repository.SubscriptionRow{
		ID:         "sub-2",
		PlanID:     "plan-missing",
		TenantID:   "tenant-1",
		CustomerID: "cust-2",
		Status:     "active",
		Amount:     "999",
		Currency:   "EUR",
		Interval:   "year",
		DeletedAt:  nil,
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(sub),
		repository.NewMockPlanRepo(), // empty — no plans
	)

	detail, warnings, err := svc.GetDetail(context.Background(), "tenant-1", "cust-2", "sub-2")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if detail.Plan != nil {
		t.Error("expected Plan to be nil when plan not found")
	}
	if len(warnings) != 1 || warnings[0] != "plan not found" {
		t.Errorf("expected warnings=[\"plan not found\"], got %v", warnings)
	}
}

func TestGetDetail_SoftDeleted(t *testing.T) {
	now := time.Now()
	sub := &repository.SubscriptionRow{
		ID:         "sub-3",
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: "cust-3",
		Status:     "cancelled",
		Amount:     "500",
		Currency:   "USD",
		Interval:   "month",
		DeletedAt:  &now,
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(sub),
		repository.NewMockPlanRepo(),
	)

	_, _, err := svc.GetDetail(context.Background(), "tenant-1", "cust-3", "sub-3")
	if err != service.ErrDeleted {
		t.Errorf("expected ErrDeleted, got %v", err)
	}
}

func TestGetDetail_NotFound(t *testing.T) {
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(), // empty
		repository.NewMockPlanRepo(),
	)

	_, _, err := svc.GetDetail(context.Background(), "tenant-1", "cust-x", "sub-unknown")
	if err != service.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetDetail_UnparseableAmount(t *testing.T) {
	sub := &repository.SubscriptionRow{
		ID:         "sub-4",
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: "cust-4",
		Status:     "active",
		Amount:     "not-a-number",
		Currency:   "USD",
		Interval:   "month",
		DeletedAt:  nil,
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(sub),
		repository.NewMockPlanRepo(),
	)

	_, _, err := svc.GetDetail(context.Background(), "tenant-1", "cust-4", "sub-4")
	if err != service.ErrBillingParse {
		t.Errorf("expected ErrBillingParse, got %v", err)
	}
}

func TestGetDetail_WrongCaller(t *testing.T) {
	sub := &repository.SubscriptionRow{
		ID:         "sub-5",
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: "cust-5",
		Status:     "active",
		Amount:     "1000",
		Currency:   "USD",
		Interval:   "month",
		DeletedAt:  nil,
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(sub),
		repository.NewMockPlanRepo(),
	)

	_, _, err := svc.GetDetail(context.Background(), "tenant-1", "cust-other", "sub-5")
	if err != service.ErrForbidden {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestGetDetail_CrossTenantPrevention(t *testing.T) {
	sub := &repository.SubscriptionRow{
		ID:         "sub-6",
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: "cust-6",
		Status:     "active",
		Amount:     "1000",
		Currency:   "USD",
		Interval:   "month",
		DeletedAt:  nil,
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(sub),
		repository.NewMockPlanRepo(),
	)

	_, _, err := svc.GetDetail(context.Background(), "tenant-2", "cust-6", "sub-6")
	if err != service.ErrNotFound {
		t.Errorf("expected ErrNotFound for cross-tenant query, got %v", err)
	}
}

func TestGetDetail_NormalizesNextBillingToUTC(t *testing.T) {
	plan := &repository.PlanRow{
		ID:          "plan-utc",
		Name:        "UTC Plan",
		Amount:      "1999",
		Currency:    "usd",
		Interval:    "month",
		Description: "UTC plan",
	}
	sub := &repository.SubscriptionRow{
		ID:          "sub-utc",
		PlanID:      "plan-utc",
		TenantID:    "tenant-utc",
		CustomerID:  "cust-utc",
		Status:      "active",
		Amount:      "1999",
		Currency:    "usd",
		Interval:    "month",
		NextBilling: "2026-04-23T10:30:00+02:00",
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(sub),
		repository.NewMockPlanRepo(plan),
	)

	detail, _, err := svc.GetDetail(context.Background(), "tenant-utc", "cust-utc", "sub-utc")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if detail.BillingSummary.NextBillingDate == nil {
		t.Fatal("expected next_billing_date")
	}
	if *detail.BillingSummary.NextBillingDate != "2026-04-23T08:30:00Z" {
		t.Fatalf("unexpected normalized next_billing_date: %s", *detail.BillingSummary.NextBillingDate)
	}
}
