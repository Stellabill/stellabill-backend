package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func setupSubscriptionBatchRouter(svc service.SubscriptionService, tenantID string, roles []auth.Role, callerID string) *gin.Engine {
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
	r.POST("/subscriptions:batch", auth.RequirePermission(auth.PermManageSubscriptions), NewBatchSubscriptionHandler(svc))
	return r
}

func performBatchRequest(t *testing.T, r *gin.Engine, body any) *httptest.ResponseRecorder {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal body: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/subscriptions:batch", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

func TestBatchSubscriptionHandler_ReturnsMultiStatusForPartialFailures(t *testing.T) {
	svc := service.NewSubscriptionService(
		repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{ID: "sub-1", TenantID: "tenant-1", Status: "active"}),
		repository.NewMockPlanRepo(),
	)
	r := setupSubscriptionBatchRouter(svc, "tenant-1", []auth.Role{auth.RoleMerchant}, "merchant-1")

	rec := performBatchRequest(t, r, map[string]any{
		"operations": []map[string]string{
			{"idempotency_key": "ok-1", "subscription_id": "sub-1", "status": "paused"},
			{"idempotency_key": "", "subscription_id": "sub-1", "status": "paused"},
		},
	})

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("expected 207, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Results []struct {
			Index      int    `json:"index"`
			StatusCode int    `json:"status_code"`
			Message    string `json:"message"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.Results[0].StatusCode != http.StatusOK || resp.Results[1].StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected result statuses: %+v", resp.Results)
	}
}
