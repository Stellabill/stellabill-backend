//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"stellabill-backend/internal/repository"
)

// TestGetSubscription_HappyPath validates that the owner of an active subscription
// receives a full 200 response including plan metadata and a billing summary.
func TestGetSubscription_HappyPath(t *testing.T) {
	planID := uniqueID("plan", t, "1")
	subID := uniqueID("sub", t, "1")
	customerID := uniqueID("cust", t, "1")

	seedPlan(t, sharedPool, &repository.PlanRow{
		ID:          planID,
		Name:        "Pro Plan",
		Amount:      "2999",
		Currency:    "USD",
		Interval:    "monthly",
		Description: "The professional tier",
	})
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID:          subID,
		PlanID:      planID,
		CustomerID:  customerID,
		Status:      "active",
		Amount:      "2999",
		Currency:    "usd",
		Interval:    "monthly",
		NextBilling: "2025-04-01T00:00:00Z",
	})

	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/subscriptions/"+subID, makeTestJWT(customerID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var envelope map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}

	if envelope["api_version"] != "1" {
		t.Errorf("api_version: want %q, got %v", "1", envelope["api_version"])
	}

	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data: expected object, got %T", envelope["data"])
	}

	assertStr(t, data, "id", subID)
	assertStr(t, data, "plan_id", planID)
	assertStr(t, data, "customer", customerID)
	assertStr(t, data, "status", "active")
	assertStr(t, data, "interval", "monthly")

	plan, ok := data["plan"].(map[string]interface{})
	if !ok {
		t.Fatalf("data.plan: expected object, got %T", data["plan"])
	}
	assertStr(t, plan, "plan_id", planID)
	assertStr(t, plan, "name", "Pro Plan")
	assertStr(t, plan, "currency", "USD")

	billing, ok := data["billing_summary"].(map[string]interface{})
	if !ok {
		t.Fatalf("data.billing_summary: expected object, got %T", data["billing_summary"])
	}
	if billing["amount_cents"] != float64(2999) {
		t.Errorf("billing_summary.amount_cents: want 2999, got %v", billing["amount_cents"])
	}
	if billing["currency"] != "USD" {
		t.Errorf("billing_summary.currency: want USD, got %v", billing["currency"])
	}
	if billing["next_billing_date"] != "2025-04-01T00:00:00Z" {
		t.Errorf("billing_summary.next_billing_date: want 2025-04-01T00:00:00Z, got %v", billing["next_billing_date"])
	}
}

// TestGetSubscription_NotFound verifies that querying a non-existent ID returns 404.
func TestGetSubscription_NotFound(t *testing.T) {
	r := buildRouter(sharedPool)
	// No seed — ID simply does not exist in the DB.
	w := do(r, http.MethodGet, "/api/subscriptions/nonexistent-id-xyz", makeTestJWT("any-caller"))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
	assertErrorBody(t, w, "subscription not found")
}

// TestGetSubscription_SoftDeleted verifies that a subscription with DeletedAt set returns 410.
func TestGetSubscription_SoftDeleted(t *testing.T) {
	planID := uniqueID("plan", t, "1")
	subID := uniqueID("sub", t, "1")
	customerID := uniqueID("cust", t, "1")
	deletedAt := time.Now().UTC().Truncate(time.Second)

	seedPlan(t, sharedPool, &repository.PlanRow{
		ID: planID, Name: "Basic", Amount: "999", Currency: "USD", Interval: "monthly",
	})
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID:         subID,
		PlanID:     planID,
		CustomerID: customerID,
		Status:     "cancelled",
		Amount:     "999",
		Currency:   "USD",
		Interval:   "monthly",
		DeletedAt:  &deletedAt,
	})

	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/subscriptions/"+subID, makeTestJWT(customerID))

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d; body: %s", w.Code, w.Body.String())
	}
	assertErrorBody(t, w, "subscription has been deleted")
}

// TestGetSubscription_Forbidden verifies that a caller who does not own the
// subscription receives 403.
func TestGetSubscription_Forbidden(t *testing.T) {
	planID := uniqueID("plan", t, "1")
	subID := uniqueID("sub", t, "1")
	ownerID := uniqueID("owner", t, "1")

	seedPlan(t, sharedPool, &repository.PlanRow{
		ID: planID, Name: "Basic", Amount: "999", Currency: "USD", Interval: "monthly",
	})
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID:         subID,
		PlanID:     planID,
		CustomerID: ownerID,
		Status:     "active",
		Amount:     "999",
		Currency:   "USD",
		Interval:   "monthly",
	})

	r := buildRouter(sharedPool)
	// JWT subject is a different caller.
	w := do(r, http.MethodGet, "/api/subscriptions/"+subID, makeTestJWT("someone-else"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body: %s", w.Code, w.Body.String())
	}
	assertErrorBody(t, w, "forbidden")
}

// TestGetSubscription_NoAuthHeader verifies that missing Authorization returns 401.
func TestGetSubscription_NoAuthHeader(t *testing.T) {
	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/subscriptions/any-id", "" /* no token */)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestGetSubscription_InvalidToken verifies that a malformed JWT returns 401.
func TestGetSubscription_InvalidToken(t *testing.T) {
	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/subscriptions/any-id", "not-a-jwt-at-all")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestGetSubscription_MissingPlan verifies that when the plan referenced by a
// subscription does not exist, the response is still 200 but includes a warning.
func TestGetSubscription_MissingPlan(t *testing.T) {
	subID := uniqueID("sub", t, "1")
	customerID := uniqueID("cust", t, "1")

	// Insert subscription referencing a plan that does NOT exist in the plans table.
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID:          subID,
		PlanID:      "plan-does-not-exist",
		CustomerID:  customerID,
		Status:      "active",
		Amount:      "1499",
		Currency:    "usd",
		Interval:    "monthly",
		NextBilling: "",
	})

	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/subscriptions/"+subID, makeTestJWT(customerID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var envelope map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}

	warnings, _ := envelope["warnings"].([]interface{})
	if len(warnings) == 0 {
		t.Fatalf("expected at least one warning, got none")
	}
	found := false
	for _, w := range warnings {
		if w == "plan not found" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning %q, got: %v", "plan not found", warnings)
	}

	// Plan field must be absent / null when plan is missing.
	data, _ := envelope["data"].(map[string]interface{})
	if data["plan"] != nil {
		t.Errorf("expected data.plan to be nil when plan is missing, got: %v", data["plan"])
	}
}

// TestGetSubscription_InvalidAmount verifies that a subscription with a
// non-numeric amount string causes the service to return 500.
func TestGetSubscription_InvalidAmount(t *testing.T) {
	planID := uniqueID("plan", t, "1")
	subID := uniqueID("sub", t, "1")
	customerID := uniqueID("cust", t, "1")

	seedPlan(t, sharedPool, &repository.PlanRow{
		ID: planID, Name: "Bad Plan", Amount: "999", Currency: "USD", Interval: "monthly",
	})
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID:         subID,
		PlanID:     planID,
		CustomerID: customerID,
		Status:     "active",
		Amount:     "not-a-number", // intentionally malformed
		Currency:   "USD",
		Interval:   "monthly",
	})

	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/subscriptions/"+subID, makeTestJWT(customerID))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestGetSubscription_Concurrent fires 10 goroutines all reading the same
// subscription simultaneously to verify there are no data races or pool
// exhaustion issues under concurrent load.
func TestGetSubscription_Concurrent(t *testing.T) {
	planID := uniqueID("plan", t, "1")
	subID := uniqueID("sub", t, "1")
	customerID := uniqueID("cust", t, "1")

	seedPlan(t, sharedPool, &repository.PlanRow{
		ID: planID, Name: "Concurrent Plan", Amount: "500", Currency: "USD", Interval: "monthly",
	})
	seedSubscription(t, sharedPool, &repository.SubscriptionRow{
		ID:         subID,
		PlanID:     planID,
		CustomerID: customerID,
		Status:     "active",
		Amount:     "500",
		Currency:   "USD",
		Interval:   "monthly",
	})

	r := buildRouter(sharedPool)
	token := makeTestJWT(customerID)
	path := "/api/subscriptions/" + subID

	const goroutines = 10
	errs := make(chan string, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			w := do(r, http.MethodGet, path, token)
			if w.Code != http.StatusOK {
				errs <- fmt.Sprintf("goroutine %d: expected 200, got %d; body: %s", n, w.Code, w.Body.String())
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for e := range errs {
		t.Error(e)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func assertStr(t *testing.T, m map[string]interface{}, key, want string) {
	t.Helper()
	got, _ := m[key].(string)
	if got != want {
		t.Errorf("%s: want %q, got %q", key, want, got)
	}
}

func assertErrorBody(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body["error"] != want {
		t.Errorf("error body: want %q, got %q", want, body["error"])
	}
}

