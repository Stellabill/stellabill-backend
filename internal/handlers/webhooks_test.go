package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockInboxRepo struct {
	mock.Mock
}

func (m *MockInboxRepo) Insert(ctx context.Context, provider, msgID, sourceID string, payload []byte) error {
	args := m.Called(ctx, provider, msgID, sourceID, payload)
	return args.Error(0)
}

func withWebhookContext(eventID, provider string, body []byte, subscriberID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("webhook_event_id", eventID)
		c.Set("webhook_provider", provider)
		c.Set("webhook_raw_body", body)
		c.Request.Header.Set("X-Subscriber-ID", subscriberID)
		c.Next()
	}
}

func TestWebhookHandler_DedupAndAck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := new(MockInboxRepo)
	payload := []byte(`{"event_type": "subscription.created", "data": {"subscription_id": "sub_789"}}`)
	mockRepo.On("Insert", mock.Anything, "stripe", "msg_123", "src_456", payload).Return(nil)

	router := gin.New()
	router.Use(withWebhookContext("msg_123", "stripe", payload, "src_456"))
	router.POST("/webhooks", NewVerifiedWebhookHandler(mockRepo))

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/webhooks", bytes.NewBuffer(payload))
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusAccepted, w.Code)
	}
	mockRepo.AssertNumberOfCalls(t, "Insert", 2)
}

func TestWebhookHandler_MemoryInboxDedup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inbox := NewMemoryWebhookInbox()
	payload := []byte(`{"ok":true}`)

	router := gin.New()
	router.Use(withWebhookContext("msg_dup", "stripe", payload, "src_1"))
	router.POST("/webhooks", NewVerifiedWebhookHandler(inbox))

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/webhooks", bytes.NewBuffer(payload))
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusAccepted, w.Code)
	}
	assert.Equal(t, 1, inbox.Len(), "duplicate deliveries must be deduped")
}

func TestWebhookHandler_FastAck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inbox := NewMemoryWebhookInbox()

	router := gin.New()
	router.Use(withWebhookContext("evt_123", "stripe", []byte("{}"), "sub_456"))
	router.POST("/webhooks", NewVerifiedWebhookHandler(inbox))

	start := time.Now()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/webhooks", bytes.NewBuffer([]byte("{}")))
	router.ServeHTTP(w, req)
	elapsed := time.Since(start)

	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.JSONEq(t, `{"status": "accepted"}`, w.Body.String())
	assert.Less(t, elapsed, 50*time.Millisecond, "sync ack must complete within 50ms")
}

func TestWebhookHandler_FailsFastOnDBError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := new(MockInboxRepo)
	mockRepo.On("Insert", mock.Anything, "stripe", "evt_123", "sub_456", []byte("{}")).
		Return(errors.New("db connection timeout"))

	router := gin.New()
	router.Use(withWebhookContext("evt_123", "stripe", []byte("{}"), "sub_456"))
	router.POST("/webhooks", NewVerifiedWebhookHandler(mockRepo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/webhooks", bytes.NewBuffer([]byte("{}")))
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	mockRepo.AssertExpectations(t)
}

func TestWebhookHandler_MissingHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockInboxRepo)

	router := gin.New()
	router.POST("/webhooks", NewVerifiedWebhookHandler(mockRepo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/webhooks", bytes.NewBuffer([]byte("{}")))
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	mockRepo.AssertNotCalled(t, "Insert")
}
