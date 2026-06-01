package repository

import (
	"context"
	"errors"
	"testing"
)

func TestMockSubscriptionRepo_NotFound(t *testing.T) {
	r := NewMockSubscriptionRepo(&SubscriptionRow{ID: "s1", TenantID: "t1"})
	if _, err := r.FindByID(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := r.FindByIDAndTenant(context.Background(), "missing", "t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing, got %v", err)
	}
	if _, err := r.FindByIDAndTenant(context.Background(), "s1", "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for wrong tenant, got %v", err)
	}
	got, err := r.FindByID(context.Background(), "s1")
	if err != nil || got.ID != "s1" {
		t.Fatalf("expected s1, got %v err=%v", got, err)
	}
}

func TestMockPlanRepo_NotFound(t *testing.T) {
	r := NewMockPlanRepo(&PlanRow{ID: "p1"})
	if _, err := r.FindByID(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockStatementRepo_ListAndFilters(t *testing.T) {
	rows := []*StatementRow{
		{ID: "st1", CustomerID: "c1", SubscriptionID: "sub1", Kind: "invoice", Status: "paid", PeriodStart: "2024-01-01T00:00:00Z", PeriodEnd: "2024-01-31T23:59:59Z"},
		{ID: "st2", CustomerID: "c1", SubscriptionID: "sub2", Kind: "credit", Status: "open", PeriodStart: "2024-02-01T00:00:00Z", PeriodEnd: "2024-02-29T23:59:59Z"},
		{ID: "st3", CustomerID: "c2", SubscriptionID: "sub1", Kind: "invoice", Status: "paid", PeriodStart: "2024-01-01T00:00:00Z", PeriodEnd: "2024-01-31T23:59:59Z"},
	}
	r := NewMockStatementRepo(rows...)
	got, total, err := r.ListByCustomerID(context.Background(), "c1", StatementQuery{SubscriptionID: "sub1", Kind: "invoice", Status: "paid", StartAfter: "2023-12-01T00:00:00Z", EndBefore: "2024-12-31T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("expected 1 result, got total=%d len=%d", total, len(got))
	}

	// Filter out by status
	_, total2, _ := r.ListByCustomerID(context.Background(), "c1", StatementQuery{Status: "no-match"})
	if total2 != 0 {
		t.Fatalf("expected 0, got %d", total2)
	}

	// StartAfter that filters everything
	_, total3, _ := r.ListByCustomerID(context.Background(), "c1", StatementQuery{StartAfter: "2099-01-01T00:00:00Z"})
	if total3 != 0 {
		t.Fatalf("expected 0 from StartAfter, got %d", total3)
	}

	// EndBefore that filters everything
	_, total4, _ := r.ListByCustomerID(context.Background(), "c1", StatementQuery{EndBefore: "2000-01-01T00:00:00Z"})
	if total4 != 0 {
		t.Fatalf("expected 0 from EndBefore, got %d", total4)
	}

	// Limit truncation
	r.records = make(map[string]*StatementRow)
	for i := 0; i < 15; i++ {
		id := "x"
		for j := 0; j < i; j++ {
			id += "x"
		}
		r.records[id] = &StatementRow{ID: id, CustomerID: "c1"}
	}
	gotLim, _, _ := r.ListByCustomerID(context.Background(), "c1", StatementQuery{Limit: 5})
	if len(gotLim) != 5 {
		t.Fatalf("expected 5, got %d", len(gotLim))
	}

	// list err and find err
	r.SetListError(errors.New("boom"))
	if _, _, err := r.ListByCustomerID(context.Background(), "c1", StatementQuery{}); err == nil {
		t.Fatal("expected list error")
	}
	r.SetFindError(errors.New("boom"))
	if _, err := r.FindByID(context.Background(), "any"); err == nil {
		t.Fatal("expected find error")
	}
	// Reset to test happy path FindByID not found
	r2 := NewMockStatementRepo()
	if _, err := r2.FindByID(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
