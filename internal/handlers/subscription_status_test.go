package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func setupSubscriptionStatusRouter(svc service.SubscriptionService, tenantID string, roles []auth.Role, callerID string) *gin.Engine {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if tenantID != "" {
			c.Set("tenantID", tenantID)
		}
		if len(roles) > 0 {
			c.Set(auth.RolesContextKey, roles)
		}
		if callerID != "" {
			c.Set("callerID", callerID)
		}
		c.Next()
	})
	r.POST("/subscriptions/:id/status", auth.RequirePermission(auth.PermManageSubscriptions), NewChangeSubscriptionStatusHandler(svc))
	return r
}

func performStatusChangeRequest(t *testing.T, r *gin.Engine, id string, body any) *httptest.ResponseRecorder {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal body: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/subscriptions/"+id+"/status", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

func TestChangeSubscriptionStatusHandler_Success(t *testing.T) {
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
			ID:       "sub-1",
			TenantID: "tenant-1",
			Status:   "active",
		}),
		repository.NewMockPlanRepo(),
	)
	r := setupSubscriptionStatusRouter(svc, "tenant-1", []auth.Role{auth.RoleMerchant}, "merchant-1")

	rec := performStatusChangeRequest(t, r, "sub-1", map[string]string{"status": "paused"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		APIVersion string                           `json:"api_version"`
		Data       service.SubscriptionStatusChange `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.APIVersion != "v1" {
		t.Fatalf("expected api_version v1, got %q", resp.APIVersion)
	}
	if resp.Data.PreviousStatus != "active" || resp.Data.Status != "paused" || !resp.Data.Changed {
		t.Fatalf("unexpected response payload: %+v", resp.Data)
	}
}

func TestChangeSubscriptionStatusHandler_InvalidTransitionConflict(t *testing.T) {
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
			ID:       "sub-2",
			TenantID: "tenant-1",
			Status:   "cancelled",
		}),
		repository.NewMockPlanRepo(),
	)
	r := setupSubscriptionStatusRouter(svc, "tenant-1", []auth.Role{auth.RoleMerchant}, "merchant-1")

	rec := performStatusChangeRequest(t, r, "sub-2", map[string]string{"status": "active"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Code != string(ErrorCodeConflict) {
		t.Fatalf("expected conflict code, got %q", resp.Code)
	}
	if resp.Message == "" {
		t.Fatal("expected clear error message")
	}
}

func TestChangeSubscriptionStatusHandler_UnknownCurrentStateConflict(t *testing.T) {
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
			ID:       "sub-3",
			TenantID: "tenant-1",
			Status:   "mystery",
		}),
		repository.NewMockPlanRepo(),
	)
	r := setupSubscriptionStatusRouter(svc, "tenant-1", []auth.Role{auth.RoleMerchant}, "merchant-1")

	rec := performStatusChangeRequest(t, r, "sub-3", map[string]string{"status": "active"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChangeSubscriptionStatusHandler_UnknownTargetStatusValidation(t *testing.T) {
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
			ID:       "sub-4",
			TenantID: "tenant-1",
			Status:   "active",
		}),
		repository.NewMockPlanRepo(),
	)
	r := setupSubscriptionStatusRouter(svc, "tenant-1", []auth.Role{auth.RoleMerchant}, "merchant-1")

	rec := performStatusChangeRequest(t, r, "sub-4", map[string]string{"status": "bogus"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Code != string(ErrorCodeValidationFailed) {
		t.Fatalf("expected validation error code, got %q", resp.Code)
	}
}

func TestChangeSubscriptionStatusHandler_RequiresStatusValue(t *testing.T) {
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
			ID:       "sub-4b",
			TenantID: "tenant-1",
			Status:   "active",
		}),
		repository.NewMockPlanRepo(),
	)
	r := setupSubscriptionStatusRouter(svc, "tenant-1", []auth.Role{auth.RoleMerchant}, "merchant-1")

	rec := performStatusChangeRequest(t, r, "sub-4b", map[string]string{"status": "   "})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChangeSubscriptionStatusHandler_NoOpSameStatus(t *testing.T) {
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
			ID:       "sub-5",
			TenantID: "tenant-1",
			Status:   "active",
		}),
		repository.NewMockPlanRepo(),
	)
	r := setupSubscriptionStatusRouter(svc, "tenant-1", []auth.Role{auth.RoleMerchant}, "merchant-1")

	rec := performStatusChangeRequest(t, r, "sub-5", map[string]string{"status": "active"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data service.SubscriptionStatusChange `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Data.Changed {
		t.Fatalf("expected changed=false for no-op, got %+v", resp.Data)
	}
}

func TestChangeSubscriptionStatusHandler_RequiresManagePermission(t *testing.T) {
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
			ID:       "sub-6",
			TenantID: "tenant-1",
			Status:   "active",
		}),
		repository.NewMockPlanRepo(),
	)

	rUnauthorized := setupSubscriptionStatusRouter(svc, "tenant-1", nil, "merchant-1")
	rec := performStatusChangeRequest(t, rUnauthorized, "sub-6", map[string]string{"status": "paused"})
	
	if rec.Code != http.StatusForbidden { 
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	rForbidden := setupSubscriptionStatusRouter(svc, "tenant-1", []auth.Role{auth.RoleCustomer}, "cust-1")
	rec = performStatusChangeRequest(t, rForbidden, "sub-6", map[string]string{"status": "paused"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChangeSubscriptionStatusHandler_RequiresTenantContext(t *testing.T) {
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
			ID:       "sub-7",
			TenantID: "tenant-1",
			Status:   "active",
		}),
		repository.NewMockPlanRepo(),
	)
	r := setupSubscriptionStatusRouter(svc, "", []auth.Role{auth.RoleMerchant}, "merchant-1")

	rec := performStatusChangeRequest(t, r, "sub-7", map[string]string{"status": "paused"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChangeSubscriptionStatusHandler_DeletedSubscription(t *testing.T) {
	now := time.Now()
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{
			ID:        "sub-8",
			TenantID:  "tenant-1",
			Status:    "active",
			DeletedAt: &now,
		}),
		repository.NewMockPlanRepo(),
	)
	r := setupSubscriptionStatusRouter(svc, "tenant-1", []auth.Role{auth.RoleMerchant}, "merchant-1")

	rec := performStatusChangeRequest(t, r, "sub-8", map[string]string{"status": "paused"})
	if rec.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d: %s", rec.Code, rec.Body.String())
	}
}
