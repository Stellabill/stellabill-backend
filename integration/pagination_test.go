//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"stellarbill-backend/internal/repository"
)

// TestListStatements_Pagination validates pagination parameters work correctly.
func TestListStatements_Pagination(t *testing.T) {
	planID := uniqueID("plan", t, "1")
	subID := uniqueID("sub", t, "1")
	customerID := uniqueID("cust", t, "1")

	seedPlan(t, sharedPool, &repository.PlanRow{
		ID: planID, Name: "Pro Plan", Amount: "2999", Currency: "USD", Interval: "monthly",
	})
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID: subID, PlanID: planID, CustomerID: customerID, Status: "active",
		Amount: "2999", Currency: "usd", Interval: "monthly", NextBilling: "2025-04-01T00:00:00Z",
	})

	// Seed 15 statements to test pagination
	for i := 0; i < 15; i++ {
		stmtID := uniqueID("stmt", t, string(rune('0'+i)))
		seedStatement(t, sharedPool, &repository.StatementRow{
			ID: stmtID, SubscriptionID: subID, CustomerID: customerID,
			PeriodStart: "2024-01-01T00:00:00Z", PeriodEnd: "2024-02-01T00:00:00Z",
			IssuedAt: "2024-02-02T00:00:00Z", TotalAmount: "2999", Currency: "USD",
			Kind: "invoice", Status: "paid",
		})
	}

	r := buildRouter(sharedPool)
	token := makeTestJWT(customerID)

	// Test page size limit
	w := do(r, http.MethodGet, "/api/statements?page_size=5", token)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var envelope map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data, ok := envelope["data"].([]interface{})
	if !ok {
		t.Fatalf("data: expected array, got %T", envelope["data"])
	}
	if len(data) != 5 {
		t.Errorf("page_size=5: expected 5 items, got %d", len(data))
	}
	pagination, ok := envelope["pagination"].(map[string]interface{})
	if !ok {
		t.Fatalf("pagination: expected object, got %T", envelope["pagination"])
	}
	if pagination["count"] != float64(15) {
		t.Errorf("pagination.count: expected 15, got %v", pagination["count"])
	}

	// Test page parameter
	w = do(r, http.MethodGet, "/api/statements?page=2&page_size=5", token)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data, ok = envelope["data"].([]interface{})
	if !ok {
		t.Fatalf("data: expected array, got %T", envelope["data"])
	}
	if len(data) != 5 {
		t.Errorf("page=2: expected 5 items, got %d", len(data))
	}
	pagination, ok = envelope["pagination"].(map[string]interface{})
	if !ok {
		t.Fatalf("pagination: expected object, got %T", envelope["pagination"])
	}
	if pagination["page"] != float64(2) {
		t.Errorf("pagination.page: expected 2, got %v", pagination["page"])
	}
}

// TestListStatements_Filtering validates filtering by subscription_id, kind, and status.
func TestListStatements_Filtering(t *testing.T) {
	planID := uniqueID("plan", t, "1")
	subID1 := uniqueID("sub", t, "1")
	subID2 := uniqueID("sub", t, "2")
	customerID := uniqueID("cust", t, "1")

	seedPlan(t, sharedPool, &repository.PlanRow{
		ID: planID, Name: "Pro Plan", Amount: "2999", Currency: "USD", Interval: "monthly",
	})
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID: subID1, PlanID: planID, CustomerID: customerID, Status: "active",
		Amount: "2999", Currency: "usd", Interval: "monthly", NextBilling: "2025-04-01T00:00:00Z",
	})
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID: subID2, PlanID: planID, CustomerID: customerID, Status: "active",
		Amount: "2999", Currency: "usd", Interval: "monthly", NextBilling: "2025-04-01T00:00:00Z",
	})

	// Seed statements with different attributes
	seedStatement(t, sharedPool, &repository.StatementRow{
		ID: uniqueID("stmt", t, "1"), SubscriptionID: subID1, CustomerID: customerID,
		PeriodStart: "2024-01-01T00:00:00Z", PeriodEnd: "2024-02-01T00:00:00Z",
		IssuedAt: "2024-02-02T00:00:00Z", TotalAmount: "2999", Currency: "USD",
		Kind: "invoice", Status: "paid",
	})
	seedStatement(t, sharedPool, &repository.StatementRow{
		ID: uniqueID("stmt", t, "2"), SubscriptionID: subID2, CustomerID: customerID,
		PeriodStart: "2024-02-01T00:00:00Z", PeriodEnd: "2024-03-01T00:00:00Z",
		IssuedAt: "2024-03-02T00:00:00Z", TotalAmount: "2999", Currency: "USD",
		Kind: "credit_note", Status: "open",
	})
	seedStatement(t, sharedPool, &repository.StatementRow{
		ID: uniqueID("stmt", t, "3"), SubscriptionID: subID1, CustomerID: customerID,
		PeriodStart: "2024-03-01T00:00:00Z", PeriodEnd: "2024-04-01T00:00:00Z",
		IssuedAt: "2024-04-02T00:00:00Z", TotalAmount: "2999", Currency: "USD",
		Kind: "invoice", Status: "open",
	})

	r := buildRouter(sharedPool)
	token := makeTestJWT(customerID)

	// Filter by subscription_id
	w := do(r, http.MethodGet, "/api/statements?subscription_id="+subID1, token)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var envelope map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data, ok := envelope["data"].([]interface{})
	if !ok {
		t.Fatalf("data: expected array, got %T", envelope["data"])
	}
	if len(data) != 2 {
		t.Errorf("filter by subscription_id: expected 2 items, got %d", len(data))
	}

	// Filter by kind
	w = do(r, http.MethodGet, "/api/statements?kind=invoice", token)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data, ok = envelope["data"].([]interface{})
	if !ok {
		t.Fatalf("data: expected array, got %T", envelope["data"])
	}
	if len(data) != 2 {
		t.Errorf("filter by kind=invoice: expected 2 items, got %d", len(data))
	}

	// Filter by status
	w = do(r, http.MethodGet, "/api/statements?status=open", token)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data, ok = envelope["data"].([]interface{})
	if !ok {
		t.Fatalf("data: expected array, got %T", envelope["data"])
	}
	if len(data) != 2 {
		t.Errorf("filter by status=open: expected 2 items, got %d", len(data))
	}
}
