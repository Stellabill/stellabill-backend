package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"stellarbill-backend/internal/service"
)

// mockSubscriptionService is a test double for service.SubscriptionService.
type mockSubscriptionService struct {
	detail   *service.SubscriptionDetail
	warnings []string
	err      error
}

func (m *mockSubscriptionService) GetDetail(_ context.Context, _, _ string) (*service.SubscriptionDetail, []string, error) {
	return m.detail, m.warnings, m.err
}

// setupRouter builds a minimal Gin router with the handler wired up.
// If setCallerID is true, a middleware injects "callerID" into the context.
func setupRouter(svc service.SubscriptionService, setCallerID bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if setCallerID {
		r.Use(func(c *gin.Context) {
			c.Set("callerID", "caller-123")
			c.Next()
		})
	}
	r.GET("/api/subscriptions/:id", NewGetSubscriptionHandler(svc))
	return r
}

func TestGetSubscription_MissingCallerID_Returns401(t *testing.T) {
	svc := &mockSubscriptionService{}
	r := setupRouter(svc, false) // no callerID injected

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] == "" {
		t.Error("expected error field in response body")
	}
}

func TestGetSubscription_EmptyID_Returns400(t *testing.T) {
	// Gin strips trailing slashes, so we test whitespace-only via a custom param.
	// We use a route that accepts a whitespace id by registering a wildcard.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("callerID", "caller-123")
		c.Next()
	})
	// Register a route that captures whitespace as the id segment.
	r.GET("/api/subscriptions/:id", NewGetSubscriptionHandler(&mockSubscriptionService{}))

	w := httptest.NewRecorder()
	// Send a request with only spaces as the id (URL-encoded space = %20).
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/%20", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] == "" {
		t.Error("expected error field in response body")
	}
}

func TestGetSubscription_ErrNotFound_Returns404(t *testing.T) {
	svc := &mockSubscriptionService{err: service.ErrNotFound}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/unknown-id", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] == "" {
		t.Error("expected error field in response body")
	}
}

func TestGetSubscription_ErrDeleted_Returns410(t *testing.T) {
	svc := &mockSubscriptionService{err: service.ErrDeleted}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/deleted-id", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] != "subscription has been deleted" {
		t.Errorf("unexpected error message: %q", body["error"])
	}
}

func TestGetSubscription_ErrForbidden_Returns403(t *testing.T) {
	svc := &mockSubscriptionService{err: service.ErrForbidden}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] == "" {
		t.Error("expected error field in response body")
	}
}

func TestGetSubscription_ErrBillingParse_Returns500(t *testing.T) {
	svc := &mockSubscriptionService{err: service.ErrBillingParse}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["error"] == "" {
		t.Error("expected error field in response body")
	}
}

func TestGetSubscription_HappyPath_Returns200WithEnvelope(t *testing.T) {
	nextBilling := "2024-02-01T00:00:00Z"
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
	svc := &mockSubscriptionService{detail: detail, warnings: nil}
	r := setupRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/sub-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Check Content-Type header.
	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %q", ct)
	}

	// Decode and verify envelope shape.
	var envelope map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&envelope); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if envelope["api_version"] != "1" {
		t.Errorf("expected api_version=1, got %v", envelope["api_version"])
	}

	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data field to be an object")
	}
	if data["id"] != "sub-1" {
		t.Errorf("expected data.id=sub-1, got %v", data["id"])
	}
	if data["plan_id"] != "plan-1" {
		t.Errorf("expected data.plan_id=plan-1, got %v", data["plan_id"])
	}
	if data["customer"] != "caller-123" {
		t.Errorf("expected data.customer=caller-123, got %v", data["customer"])
	}
	if data["status"] != "active" {
		t.Errorf("expected data.status=active, got %v", data["status"])
	}
	if data["interval"] != "monthly" {
		t.Errorf("expected data.interval=monthly, got %v", data["interval"])
	}

	// Check plan metadata embedded.
	plan, ok := data["plan"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data.plan to be an object")
	}
	if plan["plan_id"] != "plan-1" {
		t.Errorf("expected plan.plan_id=plan-1, got %v", plan["plan_id"])
	}

	// Check billing_summary.
	billing, ok := data["billing_summary"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data.billing_summary to be an object")
	}
	if billing["amount_cents"] != float64(1999) {
		t.Errorf("expected billing_summary.amount_cents=1999, got %v", billing["amount_cents"])
	}
	if billing["currency"] != "USD" {
		t.Errorf("expected billing_summary.currency=USD, got %v", billing["currency"])
	}
}
