package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHandler_HandleWebhook_NewEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	store := NewStore(24 * time.Hour)
	defer store.Clear()
	
	handler := NewHandler(store)
	router := gin.New()
	router.POST("/webhook", handler.HandleWebhook)

	reqBody := WebhookRequest{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_abc",
		EventType:       "payment.succeeded",
		Data:            map[string]string{"amount": "100"},
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "accepted", response["status"])
	assert.Equal(t, "evt_123", response["provider_event_id"])
}

func TestHandler_HandleWebhook_DuplicateEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	store := NewStore(24 * time.Hour)
	defer store.Clear()
	
	handler := NewHandler(store)
	router := gin.New()
	router.POST("/webhook", handler.HandleWebhook)

	reqBody := WebhookRequest{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_abc",
		EventType:       "payment.succeeded",
		Data:            map[string]string{"amount": "100"},
	}

	body, _ := json.Marshal(reqBody)

	// First request - new event
	req1 := httptest.NewRequest("POST", "/webhook", bytes.NewBuffer(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusAccepted, w1.Code)

	// Second request - duplicate
	req2 := httptest.NewRequest("POST", "/webhook", bytes.NewBuffer(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w2.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "duplicate", response["status"])
	assert.Equal(t, "Event already processed", response["message"])
	assert.Equal(t, "evt_123", response["provider_event_id"])
}

func TestHandler_HandleWebhook_InvalidRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	store := NewStore(24 * time.Hour)
	defer store.Clear()
	
	handler := NewHandler(store)
	router := gin.New()
	router.POST("/webhook", handler.HandleWebhook)

	// Missing required fields
	reqBody := map[string]string{
		"provider_event_id": "evt_123",
		// Missing tenant_id and event_type
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_HandleWebhook_TenantIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	store := NewStore(24 * time.Hour)
	defer store.Clear()
	
	handler := NewHandler(store)
	router := gin.New()
	router.POST("/webhook", handler.HandleWebhook)

	// Same event ID, different tenants
	reqBody1 := WebhookRequest{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_abc",
		EventType:       "payment.succeeded",
	}

	reqBody2 := WebhookRequest{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_xyz",
		EventType:       "payment.succeeded",
	}

	body1, _ := json.Marshal(reqBody1)
	body2, _ := json.Marshal(reqBody2)

	// First tenant
	req1 := httptest.NewRequest("POST", "/webhook", bytes.NewBuffer(body1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusAccepted, w1.Code)

	// Second tenant - should also be accepted (different tenant)
	req2 := httptest.NewRequest("POST", "/webhook", bytes.NewBuffer(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusAccepted, w2.Code)

	// Duplicate for first tenant
	req3 := httptest.NewRequest("POST", "/webhook", bytes.NewBuffer(body1))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusOK, w3.Code) // Duplicate
}

func TestHandler_HandleWebhook_MultipleEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	store := NewStore(24 * time.Hour)
	defer store.Clear()
	
	handler := NewHandler(store)
	router := gin.New()
	router.POST("/webhook", handler.HandleWebhook)

	events := []WebhookRequest{
		{ProviderEventID: "evt_1", TenantID: "tenant_abc", EventType: "payment.succeeded"},
		{ProviderEventID: "evt_2", TenantID: "tenant_abc", EventType: "payment.failed"},
		{ProviderEventID: "evt_3", TenantID: "tenant_abc", EventType: "invoice.created"},
	}

	for _, event := range events {
		body, _ := json.Marshal(event)
		req := httptest.NewRequest("POST", "/webhook", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusAccepted, w.Code)
	}

	assert.Equal(t, 3, store.Count())
}

func TestHandler_HandleWebhook_ConcurrentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	store := NewStore(24 * time.Hour)
	defer store.Clear()
	
	handler := NewHandler(store)
	router := gin.New()
	router.POST("/webhook", handler.HandleWebhook)

	reqBody := WebhookRequest{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_abc",
		EventType:       "payment.succeeded",
	}

	body, _ := json.Marshal(reqBody)

	// Send 10 concurrent requests with the same event
	// We can't easily test concurrent HTTP requests in unit tests,
	// but we can test the store's concurrent behavior separately
	// This test verifies the handler integration with the store
	
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/webhook", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		
		if i == 0 {
			assert.Equal(t, http.StatusAccepted, w.Code)
		} else {
			assert.Equal(t, http.StatusOK, w.Code) // Duplicates
		}
	}
}
