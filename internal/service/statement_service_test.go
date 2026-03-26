package service_test

import (
	"context"
	"testing"
	"time"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func seedStatements() []*repository.StatementRow {
	return []*repository.StatementRow{
		{
			ID:             "stmt-1",
			SubscriptionID: "sub-1",
			CustomerID:     "cust-1",
			PeriodStart:    "2024-01-01T00:00:00Z",
			PeriodEnd:      "2024-02-01T00:00:00Z",
			IssuedAt:       "2024-02-02T00:00:00Z",
			TotalAmount:    "2999",
			Currency:       "USD",
			Kind:           "invoice",
			Status:         "paid",
		},
		{
			ID:             "stmt-2",
			SubscriptionID: "sub-1",
			CustomerID:     "cust-1",
			PeriodStart:    "2024-02-01T00:00:00Z",
			PeriodEnd:      "2024-03-01T00:00:00Z",
			IssuedAt:       "2024-03-02T00:00:00Z",
			TotalAmount:    "2999",
			Currency:       "USD",
			Kind:           "invoice",
			Status:         "pending",
		},
		{
			ID:             "stmt-3",
			SubscriptionID: "sub-2",
			CustomerID:     "cust-2",
			PeriodStart:    "2024-01-01T00:00:00Z",
			PeriodEnd:      "2024-02-01T00:00:00Z",
			IssuedAt:       "2024-02-02T00:00:00Z",
			TotalAmount:    "999",
			Currency:       "EUR",
			Kind:           "credit_note",
			Status:         "paid",
		},
	}
}

func newStatementService(rows ...*repository.StatementRow) service.StatementService {
	subRepo := repository.NewMockSubscriptionRepo()
	stmtRepo := repository.NewMockStatementRepo(rows...)
	return service.NewStatementService(subRepo, stmtRepo)
}

func TestStatementGetDetail_HappyPath(t *testing.T) {
	rows := seedStatements()
	svc := newStatementService(rows...)

	detail, warnings, err := svc.GetDetail(context.Background(), "cust-1", "stmt-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}

	if detail.ID != "stmt-1" {
		t.Errorf("ID: got %q, want %q", detail.ID, "stmt-1")
	}
	if detail.SubscriptionID != "sub-1" {
		t.Errorf("SubscriptionID: got %q, want %q", detail.SubscriptionID, "sub-1")
	}
	if detail.Customer != "cust-1" {
		t.Errorf("Customer: got %q, want %q", detail.Customer, "cust-1")
	}
	if detail.PeriodStart != "2024-01-01T00:00:00Z" {
		t.Errorf("PeriodStart: got %q, want %q", detail.PeriodStart, "2024-01-01T00:00:00Z")
	}
	if detail.PeriodEnd != "2024-02-01T00:00:00Z" {
		t.Errorf("PeriodEnd: got %q, want %q", detail.PeriodEnd, "2024-02-01T00:00:00Z")
	}
	if detail.IssuedAt != "2024-02-02T00:00:00Z" {
		t.Errorf("IssuedAt: got %q, want %q", detail.IssuedAt, "2024-02-02T00:00:00Z")
	}
	if detail.TotalAmount != "2999" {
		t.Errorf("TotalAmount: got %q, want %q", detail.TotalAmount, "2999")
	}
	if detail.Currency != "USD" {
		t.Errorf("Currency: got %q, want %q", detail.Currency, "USD")
	}
	if detail.Kind != "invoice" {
		t.Errorf("Kind: got %q, want %q", detail.Kind, "invoice")
	}
	if detail.Status != "paid" {
		t.Errorf("Status: got %q, want %q", detail.Status, "paid")
	}
}

func TestStatementGetDetail_NotFound(t *testing.T) {
	svc := newStatementService() // empty repo

	_, _, err := svc.GetDetail(context.Background(), "cust-1", "stmt-missing")
	if err != service.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStatementGetDetail_SoftDeleted(t *testing.T) {
	now := time.Now()
	row := &repository.StatementRow{
		ID:             "stmt-del",
		SubscriptionID: "sub-1",
		CustomerID:     "cust-1",
		PeriodStart:    "2024-01-01T00:00:00Z",
		PeriodEnd:      "2024-02-01T00:00:00Z",
		IssuedAt:       "2024-02-02T00:00:00Z",
		TotalAmount:    "2999",
		Currency:       "USD",
		Kind:           "invoice",
		Status:         "paid",
		DeletedAt:      &now,
	}
	svc := newStatementService(row)

	_, _, err := svc.GetDetail(context.Background(), "cust-1", "stmt-del")
	if err != service.ErrDeleted {
		t.Errorf("expected ErrDeleted, got %v", err)
	}
}

func TestStatementGetDetail_WrongCaller(t *testing.T) {
	rows := seedStatements()
	svc := newStatementService(rows...)

	_, _, err := svc.GetDetail(context.Background(), "cust-other", "stmt-1")
	if err != service.ErrForbidden {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestStatementListByCustomer_HappyPath(t *testing.T) {
	rows := seedStatements()
	svc := newStatementService(rows...)

	q := repository.StatementQuery{Page: 1, PageSize: 10}
	detail, count, warnings, err := svc.ListByCustomer(context.Background(), "cust-1", "cust-1", q)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if count != 2 {
		t.Errorf("count: got %d, want 2", count)
	}
	if len(detail.Statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(detail.Statements))
	}
}

func TestStatementListByCustomer_WrongCaller(t *testing.T) {
	rows := seedStatements()
	svc := newStatementService(rows...)

	q := repository.StatementQuery{Page: 1, PageSize: 10}
	_, _, _, err := svc.ListByCustomer(context.Background(), "cust-other", "cust-1", q)
	if err != service.ErrForbidden {
		t.Errorf("expected ErrForbidden, got %v", err)
	}
}

func TestStatementListByCustomer_EmptyResult(t *testing.T) {
	svc := newStatementService() // empty repo

	q := repository.StatementQuery{Page: 1, PageSize: 10}
	detail, count, _, err := svc.ListByCustomer(context.Background(), "cust-1", "cust-1", q)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count != 0 {
		t.Errorf("count: got %d, want 0", count)
	}
	if len(detail.Statements) != 0 {
		t.Errorf("expected 0 statements, got %d", len(detail.Statements))
	}
}

func TestStatementListByCustomer_FilterByKind(t *testing.T) {
	rows := seedStatements()
	svc := newStatementService(rows...)

	q := repository.StatementQuery{Kind: "invoice", Page: 1, PageSize: 10}
	detail, count, _, err := svc.ListByCustomer(context.Background(), "cust-1", "cust-1", q)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count != 2 {
		t.Errorf("count: got %d, want 2", count)
	}
	for _, s := range detail.Statements {
		if s.Kind != "invoice" {
			t.Errorf("expected kind=invoice, got %q", s.Kind)
		}
	}
}

func TestStatementListByCustomer_FilterByStatus(t *testing.T) {
	rows := seedStatements()
	svc := newStatementService(rows...)

	q := repository.StatementQuery{Status: "pending", Page: 1, PageSize: 10}
	detail, count, _, err := svc.ListByCustomer(context.Background(), "cust-1", "cust-1", q)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count != 1 {
		t.Errorf("count: got %d, want 1", count)
	}
	if len(detail.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(detail.Statements))
	}
	if detail.Statements[0].ID != "stmt-2" {
		t.Errorf("expected stmt-2, got %q", detail.Statements[0].ID)
	}
}

func TestStatementListByCustomer_FilterBySubscriptionID(t *testing.T) {
	rows := seedStatements()
	svc := newStatementService(rows...)

	q := repository.StatementQuery{SubscriptionID: "sub-1", Page: 1, PageSize: 10}
	detail, _, _, err := svc.ListByCustomer(context.Background(), "cust-1", "cust-1", q)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	for _, s := range detail.Statements {
		if s.SubscriptionID != "sub-1" {
			t.Errorf("expected subscription_id=sub-1, got %q", s.SubscriptionID)
		}
	}
}

func TestStatementListByCustomer_Pagination(t *testing.T) {
	rows := seedStatements()
	svc := newStatementService(rows...)

	// Page 1, size 1 — should return 1 of 2 total.
	q := repository.StatementQuery{Page: 1, PageSize: 1}
	detail, count, _, err := svc.ListByCustomer(context.Background(), "cust-1", "cust-1", q)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count != 2 {
		t.Errorf("total count: got %d, want 2", count)
	}
	if len(detail.Statements) != 1 {
		t.Errorf("page size: got %d, want 1", len(detail.Statements))
	}

	// Page 2, size 1 — should return 1 of 2 total.
	q2 := repository.StatementQuery{Page: 2, PageSize: 1}
	detail2, count2, _, err := svc.ListByCustomer(context.Background(), "cust-1", "cust-1", q2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count2 != 2 {
		t.Errorf("total count: got %d, want 2", count2)
	}
	if len(detail2.Statements) != 1 {
		t.Errorf("page size: got %d, want 1", len(detail2.Statements))
	}

	// Page 3, size 1 — beyond range, should return 0.
	q3 := repository.StatementQuery{Page: 3, PageSize: 1}
	detail3, _, _, err := svc.ListByCustomer(context.Background(), "cust-1", "cust-1", q3)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(detail3.Statements) != 0 {
		t.Errorf("expected 0 statements for out-of-range page, got %d", len(detail3.Statements))
	}
}

func TestStatementListByCustomer_DefaultPagination(t *testing.T) {
	rows := seedStatements()
	svc := newStatementService(rows...)

	// Zero page/pageSize should default to page=1, pageSize=10 inside the mock.
	q := repository.StatementQuery{}
	detail, count, _, err := svc.ListByCustomer(context.Background(), "cust-1", "cust-1", q)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count != 2 {
		t.Errorf("count: got %d, want 2", count)
	}
	if len(detail.Statements) != 2 {
		t.Errorf("expected 2 statements with default pagination, got %d", len(detail.Statements))
	}
}

func TestStatementListByCustomer_DifferentCustomerIsolation(t *testing.T) {
	rows := seedStatements()
	svc := newStatementService(rows...)

	q := repository.StatementQuery{Page: 1, PageSize: 10}
	detail, count, _, err := svc.ListByCustomer(context.Background(), "cust-2", "cust-2", q)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count != 1 {
		t.Errorf("count: got %d, want 1", count)
	}
	if len(detail.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(detail.Statements))
	}
	if detail.Statements[0].Customer != "cust-2" {
		t.Errorf("expected customer=cust-2, got %q", detail.Statements[0].Customer)
	}
}

func TestStatementListByCustomer_LargeSet(t *testing.T) {
	var rows []*repository.StatementRow
	for i := 0; i < 50; i++ {
		rows = append(rows, &repository.StatementRow{
			ID:             "stmt-" + string(rune('A'+i%26)) + string(rune('0'+i/26)),
			SubscriptionID: "sub-1",
			CustomerID:     "cust-1",
			PeriodStart:    "2024-01-01T00:00:00Z",
			PeriodEnd:      "2024-02-01T00:00:00Z",
			IssuedAt:       "2024-02-02T00:00:00Z",
			TotalAmount:    "100",
			Currency:       "USD",
			Kind:           "invoice",
			Status:         "paid",
		})
	}
	svc := newStatementService(rows...)

	q := repository.StatementQuery{Page: 1, PageSize: 10}
	detail, count, _, err := svc.ListByCustomer(context.Background(), "cust-1", "cust-1", q)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if count != 50 {
		t.Errorf("total count: got %d, want 50", count)
	}
	if len(detail.Statements) != 10 {
		t.Errorf("page size: got %d, want 10", len(detail.Statements))
	}
}
