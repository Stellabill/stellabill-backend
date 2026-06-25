package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"stellarbill-backend/internal/cache"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

// newStatementServiceWithArchive creates a statement service with archival support for testing.
func newStatementServiceWithArchive(objStore cache.ObjectStore, rows ...*repository.StatementRow) service.StatementService {
	subRepo := repository.NewMockSubscriptionRepo()
	stmtRepo := repository.NewMockStatementRepo(rows...)
	return service.NewStatementServiceWithArchive(subRepo, stmtRepo, objStore)
}

func TestStatementRehydration_ArchivedStatement(t *testing.T) {
	objStore := cache.NewMemoryObjectStore()
	ctx := context.Background()

	// Create an archived statement (data cleared, archive_key set)
	now := time.Now()
	archiveKey := "statements/archive/2024/01/01/stmt-archived.json"
	archivedRow := &repository.StatementRow{
		ID:             "stmt-archived",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		// Data is cleared when archived
		PeriodStart: "",
		PeriodEnd:   "",
		IssuedAt:    "",
		TotalAmount: "",
		Currency:    "",
		Kind:        "",
		Status:      "",
		ArchivedAt:  &now,
		ArchiveKey:  archiveKey,
	}

	// Store the original data in object storage
	payload := &cache.StatementArchivePayload{
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
		ArchivedAt:     now.Format(time.RFC3339),
	}

	data, _ := json.Marshal(payload)
	objStore.Put(ctx, archiveKey, data)

	svc := newStatementServiceWithArchive(objStore, archivedRow)

	detail, warnings, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "stmt-archived")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should have rehydration warning
	if len(warnings) == 0 {
		t.Error("expected rehydration warning")
	}

	// Verify data was rehydrated
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
	objStore := cache.NewMemoryObjectStore()
	ctx := context.Background()

	// Create an archived statement with missing object storage data
	now := time.Now()
	archivedRow := &repository.StatementRow{
		ID:             "stmt-orphaned",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "", // cleared
		PeriodEnd:      "", // cleared
		IssuedAt:       "", // cleared
		TotalAmount:    "",
		Currency:       "",
		Kind:           "",
		Status:         "",
		ArchivedAt:     &now,
		ArchiveKey:     "statements/archive/2024/01/01/missing.json", // doesn't exist in store
	}

	svc := newStatementServiceWithArchive(objStore, archivedRow)

	detail, warnings, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "stmt-orphaned")
	if err != nil {
		t.Fatalf("expected no error (graceful degradation), got %v", err)
	}

	// Should have warning about failure
	if len(warnings) == 0 {
		t.Error("expected warning about rehydration failure")
	}

	// Should return stub (graceful degradation)
	if detail == nil {
		t.Error("expected detail stub, got nil")
	}
	if detail.ID != "stmt-orphaned" {
		t.Errorf("ID mismatch: got %q", detail.ID)
	}
}

func TestStatementRehydration_NoObjectStore(t *testing.T) {
	ctx := context.Background()

	// Create an archived statement but no object store (legacy mode)
	now := time.Now()
	archivedRow := &repository.StatementRow{
		ID:             "stmt-no-store",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "",
		PeriodEnd:      "",
		IssuedAt:       "",
		TotalAmount:    "",
		Currency:       "",
		Kind:           "",
		Status:         "",
		ArchivedAt:     &now,
		ArchiveKey:     "statements/archive/2024/01/01/test.json",
	}

	// Service without object store
	svc := newStatementService(archivedRow) // uses nil object store

	detail, warnings, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "stmt-no-store")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Should have no warnings (no rehydration attempted)
	if len(warnings) > 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}

	// Should return stub
	if detail == nil {
		t.Error("expected detail stub, got nil")
	}
}

func TestStatementRehydration_CacheUpdate(t *testing.T) {
	objStore := cache.NewMemoryObjectStore()
	ctx := context.Background()

	// Create an archived statement
	now := time.Now()
	archiveKey := "statements/archive/2024/01/01/stmt-cache.json"

	payload := &cache.StatementArchivePayload{
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
		ArchivedAt:     now.Format(time.RFC3339),
	}

	data, _ := json.Marshal(payload)
	objStore.Put(ctx, archiveKey, data)

	archivedRow := &repository.StatementRow{
		ID:             "stmt-cache",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "",
		PeriodEnd:      "",
		IssuedAt:       "",
		TotalAmount:    "",
		Currency:       "",
		Kind:           "",
		Status:         "",
		ArchivedAt:     &now,
		ArchiveKey:     archiveKey,
	}

	mockRepo := repository.NewMockStatementRepo(archivedRow)
	subRepo := repository.NewMockSubscriptionRepo()
	svc := service.NewStatementServiceWithArchive(subRepo, mockRepo, objStore)

	// First call - rehydrates from object storage
	_, _, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "stmt-cache")
	if err != nil {
		t.Fatalf("first GetDetail failed: %v", err)
	}

	// Verify the mock repo's UpdateArchivedData was called (cache update)
	// by checking if the in-memory record was updated
	updatedRow, _ := mockRepo.FindByID(ctx, "stmt-cache")
	if updatedRow.TotalAmount != "3000" {
		t.Errorf("Repository cache not updated: TotalAmount got %q, want %q", updatedRow.TotalAmount, "3000")
	}
}

func TestStatementRehydration_RBAC_WithArchive(t *testing.T) {
	objStore := cache.NewMemoryObjectStore()
	ctx := context.Background()

	// Create archived statement
	now := time.Now()
	archiveKey := "statements/archive/2024/01/01/stmt-rbac.json"

	payload := &cache.StatementArchivePayload{
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
		ArchivedAt:     now.Format(time.RFC3339),
	}

	data, _ := json.Marshal(payload)
	objStore.Put(ctx, archiveKey, data)

	archivedRow := &repository.StatementRow{
		ID:             "stmt-rbac",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "",
		PeriodEnd:      "",
		IssuedAt:       "",
		TotalAmount:    "",
		Currency:       "",
		Kind:           "",
		Status:         "",
		ArchivedAt:     &now,
		ArchiveKey:     archiveKey,
	}

	svc := newStatementServiceWithArchive(objStore, archivedRow)

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
	objStore := cache.NewMemoryObjectStore()
	ctx := context.Background()

	// Store corrupted JSON in object storage
	archiveKey := "statements/archive/2024/01/01/stmt-corrupt.json"
	objStore.Put(ctx, archiveKey, []byte("invalid json {"))

	now := time.Now()
	archivedRow := &repository.StatementRow{
		ID:             "stmt-corrupt",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "",
		PeriodEnd:      "",
		IssuedAt:       "",
		TotalAmount:    "",
		Currency:       "",
		Kind:           "",
		Status:         "",
		ArchivedAt:     &now,
		ArchiveKey:     archiveKey,
	}

	svc := newStatementServiceWithArchive(objStore, archivedRow)

	// Should not fail, but return stub with warning
	detail, warnings, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "stmt-corrupt")
	if err != nil {
		t.Fatalf("expected no error (graceful failure), got %v", err)
	}

	if len(warnings) == 0 {
		t.Error("expected warning about rehydration failure")
	}

	if detail == nil {
		t.Error("expected detail stub")
	}
}

func TestStatementRehydration_ContextTimeout(t *testing.T) {
	objStore := cache.NewMemoryObjectStore()

	// Create archived statement
	now := time.Now()
	archiveKey := "statements/archive/2024/01/01/stmt-timeout.json"

	payload := &cache.StatementArchivePayload{
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
		ArchivedAt:     now.Format(time.RFC3339),
	}

	data, _ := json.Marshal(payload)
	objStore.Put(context.Background(), archiveKey, data)

	archivedRow := &repository.StatementRow{
		ID:             "stmt-timeout",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "",
		PeriodEnd:      "",
		IssuedAt:       "",
		TotalAmount:    "",
		Currency:       "",
		Kind:           "",
		Status:         "",
		ArchivedAt:     &now,
		ArchiveKey:     archiveKey,
	}

	svc := newStatementServiceWithArchive(objStore, archivedRow)

	// Use cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should handle context cancellation gracefully
	detail, warnings, err := svc.GetDetail(ctx, "cust-1", []string{"customer"}, "stmt-timeout")
	if err != nil {
		t.Fatalf("expected graceful degradation, got error: %v", err)
	}

	if len(warnings) == 0 {
		t.Error("expected warning about rehydration failure due to context")
	}

	if detail == nil {
		t.Error("expected detail stub despite rehydration failure")
	}
}
