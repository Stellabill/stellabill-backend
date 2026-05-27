package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func setupSubscriptionDetailRouter(svc service.SubscriptionService, callerID, tenantID string) *gin.Engine {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if callerID != "" {
			c.Set("callerID", callerID)
		}
		if tenantID != "" {
			c.Set("tenantID", tenantID)
		}
		c.Next()
	})
	r.GET("/subscriptions/:id", NewGetSubscriptionHandler(svc))

	return r
}

func TestGetSubscriptionDetailHandler_Success(t *testing.T) {
	sub := &repository.SubscriptionRow{
		ID:          "sub-1",
		PlanID:      "plan-1",
		TenantID:    "tenant-1",
		CustomerID:  "cust-1",
		Status:      "active",
		Amount:      "2999",
		Currency:    "usd",
		Interval:    "month",
		NextBilling: "2024-08-01T00:00:00Z",
	}
	plan := &repository.PlanRow{
		ID:          "plan-1",
		Name:        "Pro",
		Amount:      "2999",
		Currency:    "usd",
		Interval:    "month",
		Description: "Pro plan",
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(sub),
		repository.NewMockPlanRepo(plan),
	)
	r := setupSubscriptionDetailRouter(svc, "cust-1", "tenant-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/subscriptions/sub-1", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		APIVersion string                     `json:"api_version"`
		Data       service.SubscriptionDetail `json:"data"`
		Warnings   []string                   `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.APIVersion != "v1" {
		t.Fatalf("expected api_version=v1, got %q", resp.APIVersion)
	}
	if resp.Data.ID != "sub-1" {
		t.Fatalf("expected detail ID sub-1, got %q", resp.Data.ID)
	}
	if resp.Data.Plan == nil || resp.Data.Plan.PlanID != "plan-1" {
		t.Fatalf("expected plan metadata in response, got %+v", resp.Data.Plan)
	}
	if resp.Data.BillingSummary.AmountCents != 2999 {
		t.Fatalf("expected amount_cents 2999, got %d", resp.Data.BillingSummary.AmountCents)
	}
	if resp.Data.BillingSummary.Currency != "USD" {
		t.Fatalf("expected billing currency USD, got %q", resp.Data.BillingSummary.Currency)
	}
	if resp.Data.BillingSummary.NextBillingDate == nil || *resp.Data.BillingSummary.NextBillingDate != "2024-08-01T00:00:00Z" {
		t.Fatalf("unexpected next_billing_date: %+v", resp.Data.BillingSummary.NextBillingDate)
	}
	if len(resp.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", resp.Warnings)
	}
}

func TestGetSubscriptionDetailHandler_MissingPlanWarning(t *testing.T) {
	sub := &repository.SubscriptionRow{
		ID:         "sub-2",
		PlanID:     "plan-missing",
		TenantID:   "tenant-1",
		CustomerID: "cust-2",
		Status:     "active",
		Amount:     "999",
		Currency:   "eur",
		Interval:   "year",
	}

	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(sub),
		repository.NewMockPlanRepo(),
	)
	r := setupSubscriptionDetailRouter(svc, "cust-2", "tenant-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/subscriptions/sub-2", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data struct {
			Plan any `json:"plan"`
		} `json:"data"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Data.Plan != nil {
		t.Fatalf("expected no plan object when plan lookup misses, got %+v", resp.Data.Plan)
	}
	if len(resp.Warnings) != 1 || resp.Warnings[0] != "plan not found" {
		t.Fatalf("expected plan warning, got %v", resp.Warnings)
	}
}

func TestGetSubscriptionDetailHandler_ErrorMappings(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		callerID   string
		tenantID   string
		pathID     string
		subRepo    repository.SubscriptionRepository
		planRepo   repository.PlanRepository
		wantStatus int
		wantCode   string
	}{
		{
			name:       "not found",
			callerID:   "cust-1",
			tenantID:   "tenant-1",
			pathID:     "missing",
			subRepo:    repository.NewMockSubscriptionRepo(),
			planRepo:   repository.NewMockPlanRepo(),
			wantStatus: http.StatusNotFound,
			wantCode:   string(ErrorCodeNotFound),
		},
		{
			name:     "wrong tenant",
			callerID: "cust-6",
			tenantID: "tenant-2",
			pathID:   "sub-6",
			subRepo: repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
				ID:         "sub-6",
				PlanID:     "plan-1",
				TenantID:   "tenant-1",
				CustomerID: "cust-6",
				Status:     "active",
				Amount:     "1000",
				Currency:   "USD",
				Interval:   "month",
			}),
			planRepo:   repository.NewMockPlanRepo(),
			wantStatus: http.StatusNotFound,
			wantCode:   string(ErrorCodeNotFound),
		},
		{
			name:     "non owner caller",
			callerID: "cust-other",
			tenantID: "tenant-1",
			pathID:   "sub-5",
			subRepo: repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
				ID:         "sub-5",
				PlanID:     "plan-1",
				TenantID:   "tenant-1",
				CustomerID: "cust-5",
				Status:     "active",
				Amount:     "1000",
				Currency:   "USD",
				Interval:   "month",
			}),
			planRepo:   repository.NewMockPlanRepo(),
			wantStatus: http.StatusForbidden,
			wantCode:   string(ErrorCodeForbidden),
		},
		{
			name:     "soft deleted",
			callerID: "cust-3",
			tenantID: "tenant-1",
			pathID:   "sub-3",
			subRepo: repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
				ID:         "sub-3",
				PlanID:     "plan-1",
				TenantID:   "tenant-1",
				CustomerID: "cust-3",
				Status:     "cancelled",
				Amount:     "500",
				Currency:   "USD",
				Interval:   "month",
				DeletedAt:  &now,
			}),
			planRepo:   repository.NewMockPlanRepo(),
			wantStatus: http.StatusGone,
			wantCode:   string(ErrorCodeNotFound),
		},
		{
			name:     "billing parse",
			callerID: "cust-4",
			tenantID: "tenant-1",
			pathID:   "sub-4",
			subRepo: repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
				ID:         "sub-4",
				PlanID:     "plan-1",
				TenantID:   "tenant-1",
				CustomerID: "cust-4",
				Status:     "active",
				Amount:     "not-a-number",
				Currency:   "USD",
				Interval:   "month",
			}),
			planRepo:   repository.NewMockPlanRepo(),
			wantStatus: http.StatusInternalServerError,
			wantCode:   string(ErrorCodeInternalError),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := service.NewSubscriptionService(tt.subRepo, tt.planRepo)
			r := setupSubscriptionDetailRouter(svc, tt.callerID, tt.tenantID)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/subscriptions/"+tt.pathID, nil)
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}

			var resp ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode error response: %v", err)
			}
			if resp.Code != tt.wantCode {
				t.Fatalf("expected error code %q, got %q", tt.wantCode, resp.Code)
			}
			if resp.TraceID == "" {
				t.Fatal("expected trace_id to be present")
			}
		})
	}
}

func TestGetSubscriptionDetailHandler_RequiresContext(t *testing.T) {
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(),
		repository.NewMockPlanRepo(),
	)

	tests := []struct {
		name       string
		callerID   string
		tenantID   string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing caller",
			tenantID:   "tenant-1",
			wantStatus: http.StatusUnauthorized,
			wantCode:   string(ErrorCodeUnauthorized),
		},
		{
			name:       "missing tenant",
			callerID:   "cust-1",
			wantStatus: http.StatusUnauthorized,
			wantCode:   string(ErrorCodeUnauthorized),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := setupSubscriptionDetailRouter(svc, tt.callerID, tt.tenantID)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/subscriptions/sub-1", nil)
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}

			var resp ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode error response: %v", err)
			}
			if resp.Code != tt.wantCode {
				t.Fatalf("expected error code %q, got %q", tt.wantCode, resp.Code)
			}
		})
	}
}
