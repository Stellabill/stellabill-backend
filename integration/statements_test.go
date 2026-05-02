//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"stellarbill-backend/internal/repository"
)

// TestGetStatement_HappyPath validates that the owner of a statement
// receives a full 200 response including subscription metadata.
func TestGetStatement_HappyPath(t *testing.T) {
	planID := uniqueID("plan", t, "1")
	subID := uniqueID("sub", t, "1")
	stmtID := uniqueID("stmt", t, "1")
	customerID := uniqueID("cust", t, "1")

	seedPlan(t, sharedPool, &repository.PlanRow{
		ID: planID, Name: "Pro Plan", Amount: "2999", Currency: "USD", Interval: "monthly",
		Description: "The professional tier",
	})
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID: subID, PlanID: planID, CustomerID: customerID, Status: "active",
		Amount: "2999", Currency: "usd", Interval: "monthly", NextBilling: "2025-04-01T00:00:00Z",
	})
	seedStatement(t, sharedPool, &repository.StatementRow{
		ID: stmtID, SubscriptionID: subID, CustomerID: customerID,
		PeriodStart: "2024-01-01T00:00:00Z", PeriodEnd: "2024-02-01T00:00:00Z",
		IssuedAt: "2024-02-02T00:00:00Z", TotalAmount: "2999", Currency: "USD",
		Kind: "invoice", Status: "paid",
	})

	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/statements/"+stmtID, makeTestJWT(customerID))

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

	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data: expected object, got %T", envelope["data"])
	}

	assertStr(t, data, "id", stmtID)
	assertStr(t, data, "subscription_id", subID)
	assertStr(t, data, "customer", customerID)
	assertStr(t, data, "kind", "invoice")
	assertStr(t, data, "status", "paid")
}

// TestGetStatement_NotFound verifies that querying a non-existent ID returns 404.
func TestGetStatement_NotFound(t *testing.T) {
	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/statements/nonexistent-id-xyz", makeTestJWT("any-caller"))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
	assertErrorBody(t, w, "statement not found")
}

// TestGetStatement_SoftDeleted verifies that a statement with DeletedAt set returns 410.
func TestGetStatement_SoftDeleted(t *testing.T) {
	subID := uniqueID("sub", t, "1")
	stmtID := uniqueID("stmt", t, "1")
	customerID := uniqueID("cust", t, "1")
	deletedAt := time.Now().UTC().Truncate(time.Second)

	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID: subID, PlanID: "plan-1", CustomerID: customerID, Status: "active",
		Amount: "999", Currency: "USD", Interval: "monthly",
	})
	seedStatement(t, sharedPool, &repository.StatementRow{
		ID: stmtID, SubscriptionID: subID, CustomerID: customerID,
		PeriodStart: "2024-01-01T00:00:00Z", PeriodEnd: "2024-02-01T00:00:00Z",
		IssuedAt: "2024-02-02T00:00:00Z", TotalAmount: "999", Currency: "USD",
		Kind: "invoice", Status: "paid", DeletedAt: &deletedAt,
	})

	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/statements/"+stmtID, makeTestJWT(customerID))

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d; body: %s", w.Code, w.Body.String())
	}
	assertErrorBody(t, w, "statement has been deleted")
}

// TestGetStatement_Forbidden verifies that a caller who does not own the
// statement receives 403.
func TestGetStatement_Forbidden(t *testing.T) {
	subID := uniqueID("sub", t, "1")
	stmtID := uniqueID("stmt", t, "1")
	ownerID := uniqueID("owner", t, "1")

	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID: subID, PlanID: "plan-1", CustomerID: ownerID, Status: "active",
		Amount: "999", Currency: "USD", Interval: "monthly",
	})
	seedStatement(t, sharedPool, &repository.StatementRow{
		ID: stmtID, SubscriptionID: subID, CustomerID: ownerID,
		PeriodStart: "2024-01-01T00:00:00Z", PeriodEnd: "2024-02-01T00:00:00Z",
		IssuedAt: "2024-02-02T00:00:00Z", TotalAmount: "999", Currency: "USD",
		Kind: "invoice", Status: "paid",
	})

	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/statements/"+stmtID, makeTestJWT("someone-else"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
	assertErrorBody(t, w, "forbidden")
}

// TestGetStatement_NoAuthHeader verifies that missing Authorization returns 401.
func TestGetStatement_NoAuthHeader(t *testing.T) {
	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/statements/any-id", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestGetStatement_InvalidToken verifies that a malformed JWT returns 401.
func TestGetStatement_InvalidToken(t *testing.T) {
	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/statements/any-id", "not-a-jwt-at-all")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}
