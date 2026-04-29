package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/openapi"
)

// validateResponseAgainstSchema unmarshals body into a generic value and validates it
// against the named schema in the OpenAPI spec.
func validateResponseAgainstSchema(t *testing.T, body []byte, schemaName string) {
	t.Helper()
	doc, err := openapi.Load()
	if err != nil {
		t.Fatalf("failed to load openapi spec: %v", err)
	}
	schemaRef, ok := doc.Components.Schemas[schemaName]
	if !ok {
		t.Fatalf("schema %q not found in spec", schemaName)
	}
	var data interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}
	if err := schemaRef.Value.VisitJSON(data); err != nil {
		t.Errorf("response does not match schema %q: %v", schemaName, err)
	}
}

func TestContract_GetSubscription_HappyPath(t *testing.T) {
	nextBilling := "2024-08-01T00:00:00Z"
	detail := &service.SubscriptionDetail{
		ID:       "sub-1",
		PlanID:   "plan-1",
		Customer: "caller-123",
		Status:   "active",
		Interval: "monthly",
		Plan: &service.PlanMetadata{
			PlanID:   "plan-1",
			Name:     "Pro",
			Amount:   "1999",
			Currency: "USD",
			Interval: "monthly",
		},
		BillingSummary: service.BillingSummary{
			AmountCents:     1999,
			Currency:        "USD",
			NextBillingDate: &nextBilling,
		},
	}
	svc := &mockSubscriptionService{detail: detail}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	validateResponseAgainstSchema(t, w.Body.Bytes(), "ResponseEnvelope")
}

func TestContract_GetSubscription_MissingPlan_Warning(t *testing.T) {
	detail := &service.SubscriptionDetail{
		ID:       "sub-2",
		PlanID:   "plan-missing",
		Customer: "caller-123",
		Status:   "active",
		Interval: "monthly",
		BillingSummary: service.BillingSummary{
			AmountCents: 999,
			Currency:    "EUR",
		},
	}
	svc := &mockSubscriptionService{detail: detail, warnings: []string{"plan not found"}}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-2", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	validateResponseAgainstSchema(t, w.Body.Bytes(), "ResponseEnvelope")
}

func TestContract_GetSubscription_ErrNotFound(t *testing.T) {
	svc := &mockSubscriptionService{err: service.ErrNotFound}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/unknown", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	validateResponseAgainstSchema(t, w.Body.Bytes(), "ErrorEnvelope")
}

func TestContract_GetSubscription_ErrDeleted(t *testing.T) {
	svc := &mockSubscriptionService{err: service.ErrDeleted}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/deleted", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", w.Code)
	}
	validateResponseAgainstSchema(t, w.Body.Bytes(), "ErrorEnvelope")
}

func TestContract_GetSubscription_ErrForbidden(t *testing.T) {
	svc := &mockSubscriptionService{err: service.ErrForbidden}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	validateResponseAgainstSchema(t, w.Body.Bytes(), "ErrorEnvelope")
}

func TestContract_GetSubscription_ErrBillingParse(t *testing.T) {
	svc := &mockSubscriptionService{err: service.ErrBillingParse}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	validateResponseAgainstSchema(t, w.Body.Bytes(), "ErrorEnvelope")
}

func TestContract_GetSubscription_MissingAuth(t *testing.T) {
	svc := &mockSubscriptionService{}
	r := setupRouter(svc, false)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	validateResponseAgainstSchema(t, w.Body.Bytes(), "ErrorEnvelope")
}

func TestContract_GetSubscription_MalformedID(t *testing.T) {
	svc := &mockSubscriptionService{}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/%20", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	validateResponseAgainstSchema(t, w.Body.Bytes(), "ErrorEnvelope")
}

func TestContract_JSONRoundTrip(t *testing.T) {
	nextBilling := "2024-08-01T00:00:00Z"
	type typedEnvelope struct {
		APIVersion string                      `json:"api_version"`
		Data       *service.SubscriptionDetail `json:"data"`
		Warnings   []string                    `json:"warnings,omitempty"`
	}

	envelope := typedEnvelope{
		APIVersion: "1",
		Data: &service.SubscriptionDetail{
			ID:       "sub-1",
			PlanID:   "plan-1",
			Customer: "caller-123",
			Status:   "active",
			Interval: "monthly",
			Plan: &service.PlanMetadata{
				PlanID:   "plan-1",
				Name:     "Pro",
				Amount:   "1999",
				Currency: "USD",
				Interval: "monthly",
			},
			BillingSummary: service.BillingSummary{
				AmountCents:     1999,
				Currency:        "USD",
				NextBillingDate: &nextBilling,
			},
		},
		Warnings: nil,
	}

	b, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded typedEnvelope
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.APIVersion != envelope.APIVersion {
		t.Errorf("api_version mismatch")
	}
	if decoded.Data == nil {
		t.Fatal("data should not be nil")
	}
	if decoded.Data.ID != envelope.Data.ID {
		t.Errorf("id mismatch")
	}
	if decoded.Data.Customer != envelope.Data.Customer {
		t.Errorf("customer mismatch")
	}
	if decoded.Data.Plan == nil {
		t.Fatal("plan should not be nil")
	}
	if decoded.Data.Plan.PlanID != envelope.Data.Plan.PlanID {
		t.Errorf("plan_id mismatch")
	}
	if decoded.Data.BillingSummary.AmountCents != envelope.Data.BillingSummary.AmountCents {
		t.Errorf("amount_cents mismatch")
	}
	if decoded.Data.BillingSummary.Currency != envelope.Data.BillingSummary.Currency {
		t.Errorf("currency mismatch")
	}
	if decoded.Data.BillingSummary.NextBillingDate == nil || *decoded.Data.BillingSummary.NextBillingDate != *envelope.Data.BillingSummary.NextBillingDate {
		t.Errorf("next_billing_date mismatch")
	}
}

func TestContract_NoSensitiveFields(t *testing.T) {
	nextBilling := "2024-08-01T00:00:00Z"
	detail := &service.SubscriptionDetail{
		ID:       "sub-1",
		PlanID:   "plan-1",
		Customer: "caller-123",
		Status:   "active",
		Interval: "monthly",
		Plan: &service.PlanMetadata{
			PlanID:   "plan-1",
			Name:     "Pro",
			Amount:   "1999",
			Currency: "USD",
			Interval: "monthly",
		},
		BillingSummary: service.BillingSummary{
			AmountCents:     1999,
			Currency:        "USD",
			NextBillingDate: &nextBilling,
		},
	}
	svc := &mockSubscriptionService{detail: detail}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	data, ok := raw["data"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data object")
	}
	for _, forbidden := range []string{"customer_id", "cost_basis", "deleted_at"} {
		if _, exists := data[forbidden]; exists {
			t.Errorf("sensitive field %q should not be present in response", forbidden)
		}
	}
}

func TestContract_Integration_HappyPath(t *testing.T) {
	const customerID = "cust-contract"
	const subID = "sub-contract"
	const planID = "plan-contract"

	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:          subID,
		PlanID:      planID,
		TenantID:    "tenant-1",
		CustomerID:  customerID,
		Status:      "active",
		Amount:      "4999",
		Currency:    "usd",
		Interval:    "monthly",
		NextBilling: "2024-12-01T00:00:00Z",
		DeletedAt:   nil,
	})
	planRepo := repository.NewMockPlanRepo(&repository.PlanRow{
		ID:          planID,
		Name:        "Contract Plan",
		Amount:      "4999",
		Currency:    "USD",
		Interval:    "monthly",
		Description: "For contract testing",
	})

	r := buildIntegrationRouter(subRepo, planRepo)
	tokenStr := makeTestJWT(customerID, "tenant-1")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/"+subID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	req.Header.Set("X-Tenant-ID", "tenant-1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	validateResponseAgainstSchema(t, w.Body.Bytes(), "ResponseEnvelope")
}

func TestContract_Integration_MissingPlan(t *testing.T) {
	const customerID = "cust-noplan"
	const subID = "sub-noplan"

	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:          subID,
		PlanID:      "missing-plan",
		TenantID:    "tenant-1",
		CustomerID:  customerID,
		Status:      "active",
		Amount:      "1000",
		Currency:    "usd",
		Interval:    "monthly",
		NextBilling: "",
		DeletedAt:   nil,
	})
	planRepo := repository.NewMockPlanRepo()

	r := buildIntegrationRouter(subRepo, planRepo)
	tokenStr := makeTestJWT(customerID, "tenant-1")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/"+subID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	req.Header.Set("X-Tenant-ID", "tenant-1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	warnings, ok := envelope["warnings"].([]interface{})
	if !ok || len(warnings) != 1 || warnings[0] != "plan not found" {
		t.Errorf("expected warnings=[\"plan not found\"], got %v", envelope["warnings"])
	}
	data := envelope["data"].(map[string]interface{})
	if _, hasPlan := data["plan"]; hasPlan {
		t.Error("expected no plan object when plan is missing")
	}
	validateResponseAgainstSchema(t, w.Body.Bytes(), "ResponseEnvelope")
}

func TestContract_Integration_SoftDeleted(t *testing.T) {
	const customerID = "cust-del"
	const subID = "sub-del"
	now := time.Now()

	subRepo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
		ID:         subID,
		PlanID:     "plan-1",
		TenantID:   "tenant-1",
		CustomerID: customerID,
		Status:     "cancelled",
		Amount:     "1000",
		Currency:   "USD",
		Interval:   "monthly",
		DeletedAt:  &now,
	})
	planRepo := repository.NewMockPlanRepo()

	r := buildIntegrationRouter(subRepo, planRepo)
	tokenStr := makeTestJWT(customerID, "tenant-1")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/"+subID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	req.Header.Set("X-Tenant-ID", "tenant-1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", w.Code)
	}
	validateResponseAgainstSchema(t, w.Body.Bytes(), "ErrorEnvelope")
}
