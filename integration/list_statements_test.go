//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"stellarbill-backend/internal/repository"
)

// TestListStatements_HappyPath validates that a customer can list their statements.
func TestListStatements_HappyPath(t *testing.T) {
	planID := uniqueID("plan", t, "1")
	subID := uniqueID("sub", t, "1")
	stmtID1 := uniqueID("stmt", t, "1")
	stmtID2 := uniqueID("stmt", t, "2")
	customerID := uniqueID("cust", t, "1")

	seedPlan(t, sharedPool, &repository.PlanRow{
		ID: planID, Name: "Pro Plan", Amount: "2999", Currency: "USD", Interval: "monthly",
	})
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID: subID, PlanID: planID, CustomerID: customerID, Status: "active",
		Amount: "2999", Currency: "usd", Interval: "monthly", NextBilling: "2025-04-01T00:00:00Z",
	})
	seedStatement(t, sharedPool, &repository.StatementRow{
		ID: stmtID1, SubscriptionID: subID, CustomerID: customerID,
		PeriodStart: "2024-01-01T00:00:00Z", PeriodEnd: "2024-02-01T00:00:00Z",
		IssuedAt: "2024-02-02T00:00:00Z", TotalAmount: "2999", Currency: "USD",
		Kind: "invoice", Status: "paid",
	})
	seedStatement(t, sharedPool, &repository.StatementRow{
		ID: stmtID2, SubscriptionID: subID, CustomerID: customerID,
		PeriodStart: "2024-02-01T00:00:00Z", PeriodEnd: "2024-03-01T00:00:00Z",
		IssuedAt: "2024-03-02T00:00:00Z", TotalAmount: "2999", Currency: "USD",
		Kind: "invoice", Status: "paid",
	})

	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/statements", makeTestJWT(customerID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var envelope map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}

	if envelope["api_version"] != "2025-01-01" {
		t.Errorf("api_version: want %q, got %v", "2025-01-01", envelope["api_version"])
	}

	data, ok := envelope["data"].([]interface{})
	if !ok {
		t.Fatalf("data: expected array, got %T", envelope["data"])
	}

	if len(data) != 2 {
		t.Errorf("expected 2 statements, got %d", len(data))
	}

	pagination, ok := envelope["pagination"].(map[string]interface{})
	if !ok {
		t.Fatalf("pagination: expected object, got %T", envelope["pagination"])
	}

	if pagination["page"] != float64(1) {
		t.Errorf("pagination.page: want 1, got %v", pagination["page"])
	}
	if pagination["page_size"] != float64(10) {
		t.Errorf("pagination.page_size: want 10, got %v", pagination["page_size"])
	}
	if pagination["count"] != float64(2) {
		t.Errorf("pagination.count: want 2, got %v", pagination["count"])
	}
}

// TestListStatements_NoAuthHeader verifies that missing Authorization returns 401.
func TestListStatements_NoAuthHeader(t *testing.T) {
	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/statements", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestListStatements_InvalidToken verifies that a malformed JWT returns 401.
func TestListStatements_InvalidToken(t *testing.T) {
	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/statements", "not-a-jwt-at-all")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}
