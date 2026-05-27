package repository

import (
	"context"
	"errors"
	"fmt"
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
	statements := []*StatementRow{
		{ID: "st-01", CustomerID: "cust-1", SubscriptionID: "sub-1", Kind: "invoice", Status: "paid", PeriodStart: "2026-01-01T00:00:00Z", PeriodEnd: "2026-01-31T23:59:59Z"},
		{ID: "st-02", CustomerID: "cust-1", SubscriptionID: "sub-1", Kind: "invoice", Status: "open", PeriodStart: "2026-02-01T00:00:00Z", PeriodEnd: "2026-02-28T23:59:59Z"},
		{ID: "st-03", CustomerID: "cust-1", SubscriptionID: "sub-2", Kind: "credit", Status: "paid", PeriodStart: "2026-03-01T00:00:00Z", PeriodEnd: "2026-03-31T23:59:59Z"},
		{ID: "st-04", CustomerID: "cust-1", SubscriptionID: "sub-2", Kind: "refund", Status: "void", PeriodStart: "2026-04-01T00:00:00Z", PeriodEnd: "2026-04-30T23:59:59Z"},
		{ID: "st-05", CustomerID: "cust-2", SubscriptionID: "sub-1", Kind: "invoice", Status: "paid", PeriodStart: "2026-01-01T00:00:00Z", PeriodEnd: "2026-01-31T23:59:59Z"},
		{ID: "st-06", CustomerID: "cust-2", SubscriptionID: "sub-3", Kind: "invoice", Status: "open", PeriodStart: "2026-02-01T00:00:00Z", PeriodEnd: "2026-02-28T23:59:59Z"},
	}

	r := NewMockStatementRepo(statements...)

	tests := []struct {
		name          string
		customerID    string
		query         StatementQuery
		expectedIDs   []string
		expectedTotal int
	}{
		{
			name:          "Empty query returns all statements for customer",
			customerID:    "cust-1",
			query:         StatementQuery{},
			expectedIDs:   []string{"st-01", "st-02", "st-03", "st-04"},
			expectedTotal: 4,
		},
		{
			name:          "Customer scoping - only returns statements for cust-2",
			customerID:    "cust-2",
			query:         StatementQuery{},
			expectedIDs:   []string{"st-05", "st-06"},
			expectedTotal: 2,
		},
		{
			name:          "Filter by SubscriptionID",
			customerID:    "cust-1",
			query:         StatementQuery{SubscriptionID: "sub-1"},
			expectedIDs:   []string{"st-01", "st-02"},
			expectedTotal: 2,
		},
		{
			name:          "Filter by Kind",
			customerID:    "cust-1",
			query:         StatementQuery{Kind: "invoice"},
			expectedIDs:   []string{"st-01", "st-02"},
			expectedTotal: 2,
		},
		{
			name:          "Filter by Status",
			customerID:    "cust-1",
			query:         StatementQuery{Status: "paid"},
			expectedIDs:   []string{"st-01", "st-03"},
			expectedTotal: 2,
		},
		{
			name:          "Filter by StartAfter (strict boundary check - PeriodStart < StartAfter filters it out)",
			customerID:    "cust-1",
			query:         StatementQuery{StartAfter: "2026-02-01T00:00:00Z"},
			expectedIDs:   []string{"st-02", "st-03", "st-04"},
			expectedTotal: 3,
		},
		{
			name:          "Date boundary check - PeriodStart == StartAfter must be kept (not filtered out)",
			customerID:    "cust-1",
			query:         StatementQuery{StartAfter: "2026-01-01T00:00:00Z"},
			expectedIDs:   []string{"st-01", "st-02", "st-03", "st-04"},
			expectedTotal: 4,
		},
		{
			name:          "Filter by EndBefore (strict boundary check - PeriodEnd > EndBefore filters it out)",
			customerID:    "cust-1",
			query:         StatementQuery{EndBefore: "2026-03-31T23:59:59Z"},
			expectedIDs:   []string{"st-01", "st-02", "st-03"},
			expectedTotal: 3,
		},
		{
			name:          "Date boundary check - PeriodEnd == EndBefore must be kept (not filtered out)",
			customerID:    "cust-1",
			query:         StatementQuery{EndBefore: "2026-04-30T23:59:59Z"},
			expectedIDs:   []string{"st-01", "st-02", "st-03", "st-04"},
			expectedTotal: 4,
		},
		{
			name:          "Combination: SubscriptionID + Kind + Status",
			customerID:    "cust-1",
			query:         StatementQuery{SubscriptionID: "sub-1", Kind: "invoice", Status: "paid"},
			expectedIDs:   []string{"st-01"},
			expectedTotal: 1,
		},
		{
			name:          "Combination: StartAfter + EndBefore",
			customerID:    "cust-1",
			query:         StatementQuery{StartAfter: "2026-02-01T00:00:00Z", EndBefore: "2026-03-31T23:59:59Z"},
			expectedIDs:   []string{"st-02", "st-03"},
			expectedTotal: 2,
		},
		{
			name:          "Edge case: empty result set (no matching customer)",
			customerID:    "cust-nonexistent",
			query:         StatementQuery{},
			expectedIDs:   []string{},
			expectedTotal: 0,
		},
		{
			name:          "Edge case: empty result set (no matching status)",
			customerID:    "cust-1",
			query:         StatementQuery{Status: "nonexistent"},
			expectedIDs:   []string{},
			expectedTotal: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, total, err := r.ListByCustomerID(context.Background(), tc.customerID, tc.query)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if total != tc.expectedTotal {
				t.Errorf("expected total %d, got %d", tc.expectedTotal, total)
			}
			if len(got) != len(tc.expectedIDs) {
				t.Fatalf("expected len %d, got %d", len(tc.expectedIDs), len(got))
			}
			for i, expectedID := range tc.expectedIDs {
				if got[i].ID != expectedID {
					t.Errorf("at index %d: expected ID %s, got %s", i, expectedID, got[i].ID)
				}
			}
		})
	}
}

func TestMockStatementRepo_LimitAndTruncation(t *testing.T) {
	// Create a repository with 15 statements for "cust-1"
	var rows []*StatementRow
	for i := 1; i <= 15; i++ {
		id := fmt.Sprintf("st-%02d", i)
		rows = append(rows, &StatementRow{
			ID:         id,
			CustomerID: "cust-1",
		})
	}
	// Add one row for another customer to make sure customer scoping still works
	rows = append(rows, &StatementRow{
		ID:         "st-other",
		CustomerID: "cust-2",
	})

	r := NewMockStatementRepo(rows...)

	tests := []struct {
		name          string
		limit         int
		expectedLen   int
		expectedTotal int
		expectedIDs   []string
	}{
		{
			name:          "Default limit of 10 when limit is 0",
			limit:         0,
			expectedLen:   10,
			expectedTotal: 15,
			expectedIDs:   []string{"st-01", "st-02", "st-03", "st-04", "st-05", "st-06", "st-07", "st-08", "st-09", "st-10"},
		},
		{
			name:          "Default limit of 10 when limit is negative",
			limit:         -5,
			expectedLen:   10,
			expectedTotal: 15,
			expectedIDs:   []string{"st-01", "st-02", "st-03", "st-04", "st-05", "st-06", "st-07", "st-08", "st-09", "st-10"},
		},
		{
			name:          "Limit smaller than available (truncation) and count semantics verified",
			limit:         5,
			expectedLen:   5,
			expectedTotal: 15,
			expectedIDs:   []string{"st-01", "st-02", "st-03", "st-04", "st-05"},
		},
		{
			name:          "Limit larger than available",
			limit:         20,
			expectedLen:   15,
			expectedTotal: 15,
			expectedIDs:   []string{"st-01", "st-02", "st-03", "st-04", "st-05", "st-06", "st-07", "st-08", "st-09", "st-10", "st-11", "st-12", "st-13", "st-14", "st-15"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, total, err := r.ListByCustomerID(context.Background(), "cust-1", StatementQuery{Limit: tc.limit})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if total != tc.expectedTotal {
				t.Errorf("expected total %d, got %d", tc.expectedTotal, total)
			}
			if len(got) != tc.expectedLen {
				t.Errorf("expected len %d, got %d", tc.expectedLen, len(got))
			}
			for i, expectedID := range tc.expectedIDs {
				if got[i].ID != expectedID {
					t.Errorf("at index %d: expected ID %s, got %s", i, expectedID, got[i].ID)
				}
			}
		})
	}
}

func TestMockStatementRepo_Errors(t *testing.T) {
	r := NewMockStatementRepo(&StatementRow{ID: "st-01", CustomerID: "cust-1"})

	// 1. SetListError
	expectedListErr := errors.New("database failure on list")
	r.SetListError(expectedListErr)

	got, total, err := r.ListByCustomerID(context.Background(), "cust-1", StatementQuery{})
	if !errors.Is(err, expectedListErr) {
		t.Fatalf("expected list error %v, got %v", expectedListErr, err)
	}
	if got != nil || total != 0 {
		t.Fatalf("expected nil slice and 0 total, got slice=%v, total=%d", got, total)
	}

	// Reset list error
	r.SetListError(nil)
	got, total, err = r.ListByCustomerID(context.Background(), "cust-1", StatementQuery{})
	if err != nil {
		t.Fatalf("unexpected list error after reset: %v", err)
	}
	if len(got) != 1 || total != 1 {
		t.Fatalf("expected 1 result, got slice len=%d, total=%d", len(got), total)
	}

	// 2. SetFindError
	expectedFindErr := errors.New("database failure on find")
	r.SetFindError(expectedFindErr)

	gotRow, err := r.FindByID(context.Background(), "st-01")
	if !errors.Is(err, expectedFindErr) {
		t.Fatalf("expected find error %v, got %v", expectedFindErr, err)
	}
	if gotRow != nil {
		t.Fatalf("expected nil row, got %v", gotRow)
	}

	// Reset find error
	r.SetFindError(nil)
	gotRow, err = r.FindByID(context.Background(), "st-01")
	if err != nil {
		t.Fatalf("unexpected find error after reset: %v", err)
	}
	if gotRow.ID != "st-01" {
		t.Fatalf("expected row ID st-01, got %s", gotRow.ID)
	}
}

func TestMockStatementRepo_FindByID_NotFound(t *testing.T) {
	r := NewMockStatementRepo()
	if _, err := r.FindByID(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

