//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"stellarbill-backend/internal/repository"
)

// TestJourney_PlanToSubscribeToStatement tests the complete user journey:
// 1. List available plans
// 2. Create a subscription (simulated via seed)
// 3. View subscription details
// 4. List statements for the subscription
// 5. View a specific statement
func TestJourney_PlanToSubscribeToStatement(t *testing.T) {
	planID := uniqueID("plan", t, "1")
	subID := uniqueID("sub", t, "1")
	stmtID := uniqueID("stmt", t, "1")
	customerID := uniqueID("cust", t, "1")

	// Step 1: Seed a plan
	seedPlan(t, sharedPool, &repository.PlanRow{
		ID: planID, Name: "Pro Plan", Amount: "2999", Currency: "USD",
		Interval: "monthly", Description: "The professional tier",
	})

	// Step 2: Seed a subscription (simulating subscribe flow)
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID: subID, PlanID: planID, CustomerID: customerID, Status: "active",
		Amount: "2999", Currency: "usd", Interval: "monthly", NextBilling: "2025-04-01T00:00:00Z",
	})

	// Step 3: Seed a statement (simulating billing cycle)
	seedStatement(t, sharedPool, &repository.StatementRow{
		ID: stmtID, SubscriptionID: subID, CustomerID: customerID,
		PeriodStart: "2024-01-01T00:00:00Z", PeriodEnd: "2024-02-01T00:00:00Z",
		IssuedAt: "2024-02-02T00:00:00Z", TotalAmount: "2999", Currency: "USD",
		Kind: "invoice", Status: "paid",
	})

	r := buildRouter(sharedPool)
	token := makeTestJWT(customerID)

	// Verify: List plans includes our plan
	w := do(r, http.MethodGet, "/api/plans", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list plans: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var plansBody map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&plansBody); err != nil {
		t.Fatalf("decode plans response: %v", err)
	}
	plans, ok := plansBody["plans"].([]interface{})
	if !ok {
		t.Fatalf("plans: expected array, got %T", plansBody["plans"])
	}
	if len(plans) == 0 {
		t.Error("expected at least one plan")
	}

	// Verify: Get subscription details
	w = do(r, http.MethodGet, "/api/subscriptions/"+subID, token)
	if w.Code != http.StatusOK {
		t.Fatalf("get subscription: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var subBody map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&subBody); err != nil {
		t.Fatalf("decode subscription response: %v", err)
	}
	data, ok := subBody["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("subscription data: expected object, got %T", subBody["data"])
	}
	assertStr(t, data, "id", subID)
	assertStr(t, data, "customer", customerID)

	// Verify: List statements for customer
	w = do(r, http.MethodGet, "/api/statements", token)
	if w.Code != http.StatusOK {
		t.Fatalf("list statements: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var stmtsBody map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&stmtsBody); err != nil {
		t.Fatalf("decode statements response: %v", err)
	}
	stmts, ok := stmtsBody["data"].([]interface{})
	if !ok {
		t.Fatalf("statements data: expected array, got %T", stmtsBody["data"])
	}
	if len(stmts) != 1 {
		t.Errorf("expected 1 statement, got %d", len(stmts))
	}

	// Verify: Get specific statement details
	w = do(r, http.MethodGet, "/api/statements/"+stmtID, token)
	if w.Code != http.StatusOK {
		t.Fatalf("get statement: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var stmtBody map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&stmtBody); err != nil {
		t.Fatalf("decode statement response: %v", err)
	}
	stmtData, ok := stmtBody["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("statement data: expected object, got %T", stmtBody["data"])
	}
	assertStr(t, stmtData, "id", stmtID)
	assertStr(t, stmtData, "subscription_id", subID)
	assertStr(t, stmtData, "customer", customerID)
}

// TestJourney_CrossTenantIsolation verifies that customers cannot access
// each other's subscriptions or statements (no cross-tenant leakage).
func TestJourney_CrossTenantIsolation(t *testing.T) {
	planID := uniqueID("plan", t, "1")
	subID1 := uniqueID("sub", t, "1")
	subID2 := uniqueID("sub", t, "2")
	stmtID1 := uniqueID("stmt", t, "1")
	stmtID2 := uniqueID("stmt", t, "2")
	customer1 := uniqueID("cust", t, "1")
	customer2 := uniqueID("cust", t, "2")

	seedPlan(t, sharedPool, &repository.PlanRow{
		ID: planID, Name: "Pro Plan", Amount: "2999", Currency: "USD",
		Interval: "monthly", Description: "The professional tier",
	})

	// Customer 1's subscription and statement
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID: subID1, PlanID: planID, CustomerID: customer1, Status: "active",
		Amount: "2999", Currency: "usd", Interval: "monthly", NextBilling: "2025-04-01T00:00:00Z",
	})
	seedStatement(t, sharedPool, &repository.StatementRow{
		ID: stmtID1, SubscriptionID: subID1, CustomerID: customer1,
		PeriodStart: "2024-01-01T00:00:00Z", PeriodEnd: "2024-02-01T00:00:00Z",
		IssuedAt: "2024-02-02T00:00:00Z", TotalAmount: "2999", Currency: "USD",
		Kind: "invoice", Status: "paid",
	})

	// Customer 2's subscription and statement
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID: subID2, PlanID: planID, CustomerID: customer2, Status: "active",
		Amount: "2999", Currency: "usd", Interval: "monthly", NextBilling: "2025-04-01T00:00:00Z",
	})
	seedStatement(t, sharedPool, &repository.StatementRow{
		ID: stmtID2, SubscriptionID: subID2, CustomerID: customer2,
		PeriodStart: "2024-01-01T00:00:00Z", PeriodEnd: "2024-02-01T00:00:00Z",
		IssuedAt: "2024-02-02T00:00:00Z", TotalAmount: "2999", Currency: "USD",
		Kind: "invoice", Status: "paid",
	})

	r := buildRouter(sharedPool)
	token1 := makeTestJWT(customer1)
	token2 := makeTestJWT(customer2)

	// Customer 1 cannot access Customer 2's subscription
	w := do(r, http.MethodGet, "/api/subscriptions/"+subID2, token1)
	if w.Code != http.StatusForbidden {
		t.Errorf("customer1 accessing customer2's subscription: expected 403, got %d", w.Code)
	}

	// Customer 2 cannot access Customer 1's subscription
	w = do(r, http.MethodGet, "/api/subscriptions/"+subID1, token2)
	if w.Code != http.StatusForbidden {
		t.Errorf("customer2 accessing customer1's subscription: expected 403, got %d", w.Code)
	}

	// Customer 1 cannot access Customer 2's statement
	w = do(r, http.MethodGet, "/api/statements/"+stmtID2, token1)
	if w.Code != http.StatusForbidden {
		t.Errorf("customer1 accessing customer2's statement: expected 403, got %d", w.Code)
	}

	// Customer 2 cannot access Customer 1's statement
	w = do(r, http.MethodGet, "/api/statements/"+stmtID1, token2)
	if w.Code != http.StatusForbidden {
		t.Errorf("customer2 accessing customer1's statement: expected 403, got %d", w.Code)
	}

	// Customer 1's statement list only contains their own statements
	w = do(r, http.MethodGet, "/api/statements", token1)
	if w.Code != http.StatusOK {
		t.Fatalf("customer1 list statements: expected 200, got %d", w.Code)
	}
	var stmtsBody map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&stmtsBody); err != nil {
		t.Fatalf("decode statements response: %v", err)
	}
	stmts, ok := stmtsBody["data"].([]interface{})
	if !ok {
		t.Fatalf("statements data: expected array, got %T", stmtsBody["data"])
	}
	if len(stmts) != 1 {
		t.Errorf("customer1 expected 1 statement, got %d", len(stmts))
	}

	// Customer 2's statement list only contains their own statements
	w = do(r, http.MethodGet, "/api/statements", token2)
	if w.Code != http.StatusOK {
		t.Fatalf("customer2 list statements: expected 200, got %d", w.Code)
	}
	if err := json.NewDecoder(w.Body).Decode(&stmtsBody); err != nil {
		t.Fatalf("decode statements response: %v", err)
	}
	stmts, ok = stmtsBody["data"].([]interface{})
	if !ok {
		t.Fatalf("statements data: expected array, got %T", stmtsBody["data"])
	}
	if len(stmts) != 1 {
		t.Errorf("customer2 expected 1 statement, got %d", len(stmts))
	}
}
