package service_test

import (
	"context"
	"testing"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func TestStatementRehydration_ArchivedStatement(t *testing.T) {
	ctx := context.Background()

	row := &repository.StatementRow{
		ID:             "stmt-archived",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "2023-01-01T00:00:00Z",
		PeriodEnd:      "2023-02-01T00:00:00Z",
		IssuedAt:       "2023-02-02T00:00:00Z",
		TotalAmount:    "5000",
		Currency:       "EUR",
		Kind:           "invoice",
		Status:         "paid",
	}

	subRepo := repository.NewMockSubscriptionRepo()
	stmtRepo := repository.NewMockStatementRepo(row)
	svc := service.NewStatementService(subRepo, stmtRepo)

	detail, _, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "stmt-archived")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if detail.PeriodStart != "2023-01-01T00:00:00Z" {
		t.Errorf("PeriodStart: got %q, want %q", detail.PeriodStart, "2023-01-01T00:00:00Z")
	}
	if detail.TotalAmount != "5000" {
		t.Errorf("TotalAmount: got %q, want %q", detail.TotalAmount, "5000")
	}
	if detail.Currency != "EUR" {
		t.Errorf("Currency: got %q, want %q", detail.Currency, "EUR")
	}
	if detail.Kind != "invoice" {
		t.Errorf("Kind: got %q, want %q", detail.Kind, "invoice")
	}
	if detail.Status != "paid" {
		t.Errorf("Status: got %q, want %q", detail.Status, "paid")
	}
}

func TestStatementRehydration_ArchivedNotFound(t *testing.T) {
	ctx := context.Background()

	subRepo := repository.NewMockSubscriptionRepo()
	stmtRepo := repository.NewMockStatementRepo()
	svc := service.NewStatementService(subRepo, stmtRepo)

	_, _, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "nonexistent")
	if err != service.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStatementRehydration_NoObjectStore(t *testing.T) {
	ctx := context.Background()

	row := &repository.StatementRow{
		ID:             "stmt-no-store",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "2023-01-01T00:00:00Z",
		PeriodEnd:      "2023-02-01T00:00:00Z",
		IssuedAt:       "2023-02-02T00:00:00Z",
		TotalAmount:    "1000",
		Currency:       "USD",
		Kind:           "invoice",
		Status:         "paid",
	}

	subRepo := repository.NewMockSubscriptionRepo()
	stmtRepo := repository.NewMockStatementRepo(row)
	svc := service.NewStatementService(subRepo, stmtRepo)

	detail, warnings, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "stmt-no-store")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(warnings) > 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if detail == nil {
		t.Error("expected detail, got nil")
	}
}

func TestStatementRehydration_CacheUpdate(t *testing.T) {
	ctx := context.Background()

	row := &repository.StatementRow{
		ID:             "stmt-cache",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "2023-01-01T00:00:00Z",
		PeriodEnd:      "2023-02-01T00:00:00Z",
		IssuedAt:       "2023-02-02T00:00:00Z",
		TotalAmount:    "3000",
		Currency:       "GBP",
		Kind:           "credit_note",
		Status:         "pending",
	}

	mockRepo := repository.NewMockStatementRepo(row)
	subRepo := repository.NewMockSubscriptionRepo()
	svc := service.NewStatementService(subRepo, mockRepo)

	_, _, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "stmt-cache")
	if err != nil {
		t.Fatalf("GetDetail failed: %v", err)
	}

	updatedRow, _ := mockRepo.FindByID(ctx, "stmt-cache")
	if updatedRow.TotalAmount != "3000" {
		t.Errorf("Repository record unchanged: TotalAmount got %q, want %q", updatedRow.TotalAmount, "3000")
	}
}

func TestStatementRehydration_RBAC_WithArchive(t *testing.T) {
	ctx := context.Background()

	row := &repository.StatementRow{
		ID:             "stmt-rbac",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "2023-01-01T00:00:00Z",
		PeriodEnd:      "2023-02-01T00:00:00Z",
		IssuedAt:       "2023-02-02T00:00:00Z",
		TotalAmount:    "1000",
		Currency:       "USD",
		Kind:           "invoice",
		Status:         "paid",
	}

	subRepo := repository.NewMockSubscriptionRepo()
	stmtRepo := repository.NewMockStatementRepo(row)
	svc := service.NewStatementService(subRepo, stmtRepo)

	// Unauthorized caller
	_, _, err := svc.GetDetail(ctx, "cust-unauthorized", []string{"customer"}, "stmt-rbac")
	if err != service.ErrForbidden {
		t.Errorf("expected ErrForbidden, got %v", err)
	}

	// Authorized caller
	detail, _, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "stmt-rbac")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if detail == nil {
		t.Error("expected detail, got nil")
	}
}

func TestStatementRehydration_PartialFailure(t *testing.T) {
	ctx := context.Background()

	row := &repository.StatementRow{
		ID:             "stmt-corrupt",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "2023-01-01T00:00:00Z",
		PeriodEnd:      "2023-02-01T00:00:00Z",
		IssuedAt:       "2023-02-02T00:00:00Z",
		TotalAmount:    "500",
		Currency:       "USD",
		Kind:           "invoice",
		Status:         "paid",
	}

	subRepo := repository.NewMockSubscriptionRepo()
	stmtRepo := repository.NewMockStatementRepo(row)
	svc := service.NewStatementService(subRepo, stmtRepo)

	detail, _, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "stmt-corrupt")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if detail == nil {
		t.Error("expected detail, got nil")
	}
}

func TestStatementRehydration_ContextTimeout(t *testing.T) {
	row := &repository.StatementRow{
		ID:             "stmt-timeout",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "2023-01-01T00:00:00Z",
		PeriodEnd:      "2023-02-01T00:00:00Z",
		IssuedAt:       "2023-02-02T00:00:00Z",
		TotalAmount:    "2000",
		Currency:       "USD",
		Kind:           "invoice",
		Status:         "paid",
	}

	subRepo := repository.NewMockSubscriptionRepo()
	stmtRepo := repository.NewMockStatementRepo(row)
	svc := service.NewStatementService(subRepo, stmtRepo)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	detail, _, err := svc.GetDetail(cancelledCtx, "cust-1", []string{"customer"}, "stmt-timeout")
	if err != nil {
		t.Fatalf("expected graceful degradation, got error: %v", err)
	}
	if detail == nil {
		t.Error("expected detail stub despite cancelled context")
	}
}
