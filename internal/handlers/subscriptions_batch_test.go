package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func setupBatchSubscriptionsRouter(svc service.SubscriptionService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tenantID", "tenant-1")
		c.Set("callerID", "cust-1")
		c.Next()
	})
	r.POST("/api/v1/subscriptions:batch", NewBatchSubscriptionsHandler(svc))
	return r
}

func TestBatchSubscriptionsHandler_PartialSuccess(t *testing.T) {
	repo := repository.NewMockSubscriptionRepo(
		&repository.SubscriptionRow{ID: "sub-1", TenantID: "tenant-1", CustomerID: "cust-1", Status: "active"},
		&repository.SubscriptionRow{ID: "sub-2", TenantID: "tenant-1", CustomerID: "cust-2", Status: "active"},
	)
	svc := service.NewSubscriptionService(repo, repository.NewMockPlanRepo())
	r := setupBatchSubscriptionsRouter(svc)

	body := []byte(`{"operations":[{"id":"sub-1","status":"paused","idempotency_key":"k-1"},{"id":"sub-2","status":"bogus","idempotency_key":"k-2"}]}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions:batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusMultiStatus, rec.Code)

	var resp struct {
		Data struct {
			Results []map[string]any `json:"results"`
			Summary map[string]int   `json:"summary"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data.Results, 2)
	require.Equal(t, map[string]int{"success": 1, "failed": 1}, resp.Data.Summary)
}

func TestBatchSubscriptionsHandler_RejectsOversizedBatch(t *testing.T) {
	repo := repository.NewMockSubscriptionRepo()
	svc := service.NewSubscriptionService(repo, repository.NewMockPlanRepo())
	r := setupBatchSubscriptionsRouter(svc)

	var body bytes.Buffer
	body.WriteString(`{"operations":[`)
	for i := 0; i < 101; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`{"id":"sub-` + string(rune('0'+i%10)) + `","status":"paused","idempotency_key":"k-` + string(rune('0'+i%10)) + `"}`)
	}
	body.WriteString(`]}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions:batch", &body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestBatchSubscriptionsHandler_RequiresIdempotencyKey(t *testing.T) {
	repo := repository.NewMockSubscriptionRepo(&repository.SubscriptionRow{ID: "sub-1", TenantID: "tenant-1", CustomerID: "cust-1", Status: "active"})
	svc := service.NewSubscriptionService(repo, repository.NewMockPlanRepo())
	r := setupBatchSubscriptionsRouter(svc)

	body := []byte(`{"operations":[{"id":"sub-1","status":"paused"}]}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions:batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}
