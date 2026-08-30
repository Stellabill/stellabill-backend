package handlers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"stellarbill-backend/internal/outbox"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockOutboxRepo struct {
	mock.Mock
}

type MockInboxRepo struct {
	mock.Mock
}

func (m *MockOutboxRepo) Store(ctx context.Context, event *outbox.Event) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockInboxRepo) Insert(ctx context.Context, provider, msgID, sourceID string, payload []byte) error {
	args := m.Called(ctx, provider, msgID, sourceID, payload)
	return args.Error(0)
}

func (m *MockOutboxRepo) GetPendingEvents(limit int) ([]*outbox.Event, error) { return nil, nil }
func (m *MockOutboxRepo) GetByID(id uuid.UUID) (*outbox.Event, error)         { return nil, nil }
func (m *MockOutboxRepo) UpdateStatus(id uuid.UUID, status outbox.Status, errorMessage *string) error {
	return nil
}
func (m *MockOutboxRepo) MarkAsProcessing(id uuid.UUID) error { return nil }
func (m *MockOutboxRepo) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error {
	return nil
}
func (m *MockOutboxRepo) DeleteCompletedEvents(olderThan time.Time) (int64, error)  { return 0, nil }
func (m *MockOutboxRepo) ListDeadLetteredEvents(limit int) ([]*outbox.Event, error) { return nil, nil }
func (m *MockOutboxRepo) RequeueEvent(id uuid.UUID) error                           { return nil }
func (m *MockOutboxRepo) EnsurePublisherProgressTable() error                       { return nil }
func (m *MockOutboxRepo) GetPublisherProgress(publisher string) (*uuid.UUID, error) {
	return nil, nil
}
func (m *MockOutboxRepo) GetPendingEventsForPublisher(publisher string, limit int) ([]*outbox.Event, error) {
	return nil, nil
}
func (m *MockOutboxRepo) MarkPublished(publisher string, event *outbox.Event, publishers []string) error {
	return nil
}

func TestWebhookHandler_DedupAndAck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := new(MockInboxRepo)
	payload := []byte(`{"event_type": "subscription.created", "data": {"subscription_id": "sub_789"}}`)

	mockRepo.On("Insert", mock.Anything, "stripe", "msg_123", "src_456", payload).Return(nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("webhook_event_id", "msg_123")
		c.Set("webhook_provider", "stripe")
		c.Set("webhook_raw_body", payload)
		c.Request.Header.Set("X-Subscriber-ID", "src_456")
		c.Next()
	})

	router.POST("/webhooks", NewVerifiedWebhookHandler(mockRepo))

	req1, _ := http.NewRequest(http.MethodPost, "/webhooks", bytes.NewBuffer(payload))
	rr1 := httptest.NewRecorder()

	router.ServeHTTP(rr1, req1)
	assert.Equal(t, http.StatusAccepted, rr1.Code)

	req2, _ := http.NewRequest(http.MethodPost, "/webhooks", bytes.NewBuffer(payload))
	rr2 := httptest.NewRecorder()

	router.ServeHTTP(rr2, req2)
	assert.Equal(t, http.StatusAccepted, rr2.Code)

	mockRepo.AssertNumberOfCalls(t, "Insert", 2)
}

func TestWebhookHandler_FastAck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := new(MockInboxRepo)
	mockRepo.On("Insert", mock.Anything, "stripe", "evt_123", "sub_456", []byte("{}")).Return(nil)

	router := gin.New()

	router.Use(func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_123")
		c.Set("webhook_provider", "stripe")
		c.Set("webhook_raw_body", []byte("{}"))
		c.Request.Header.Set("X-Subscriber-ID", "sub_456")
		c.Next()
	})

	router.POST("/webhooks", NewVerifiedWebhookHandler(mockRepo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/webhooks", bytes.NewBuffer([]byte("{}")))
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.JSONEq(t, `{"status": "accepted"}`, w.Body.String())
	mockRepo.AssertExpectations(t)
}

func TestWebhookHandler_FailsFastOnDBError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := new(MockInboxRepo)
	mockRepo.On("Insert", mock.Anything, "stripe", "evt_123", "sub_456", []byte("{}")).
		Return(errors.New("db connection timeout"))

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_123")
		c.Set("webhook_provider", "stripe")
		c.Set("webhook_raw_body", []byte("{}"))
		c.Request.Header.Set("X-Subscriber-ID", "sub_456")
		c.Next()
	})
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

func TestNewWebhookHandler(t *testing.T) {
	mockRepo := new(MockOutboxRepo)
	handler := NewWebhookHandler(mockRepo)
	assert.NotNil(t, handler)

	handlerNoStore := NewWebhookHandler()
	assert.NotNil(t, handlerNoStore)
}

func TestHandleWebhook_SubscriptionCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockOutboxRepo)
	mockRepo.On("Store", mock.Anything, mock.Anything).Return(nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_123")
		c.Set("webhook_provider", "stripe")
		c.Set("webhook_raw_body", []byte(`{"event_type":"subscription.created","data":{"subscription_id":"sub_123"}}`))
		c.Next()
	})
	wh := NewWebhookHandler(mockRepo)
	r.POST("/webhook", wh.Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook",
		bytes.NewBufferString(`{"event_type":"subscription.created","data":{"subscription_id":"sub_123"}}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"accepted","event_type":"subscription.created","subscription_id":"sub_123"}`, w.Body.String())
	mockRepo.AssertExpectations(t)
}

func TestHandleWebhook_StatementIssued(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockOutboxRepo)
	mockRepo.On("Store", mock.Anything, mock.Anything).Return(nil)

	r := gin.New()
	wh := NewWebhookHandler(mockRepo)
	r.POST("/webhook", wh.Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook",
		bytes.NewBufferString(`{"event_type":"statement.issued","data":{"statement_id":"stmt_456"}}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"accepted","event_type":"statement.issued","statement_id":"stmt_456"}`, w.Body.String())
	mockRepo.AssertExpectations(t)
}

func TestHandleWebhook_StoresEventWithDeduplicationID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockOutboxRepo)
	mockRepo.On("Store", mock.Anything, mock.MatchedBy(func(e *outbox.Event) bool {
		return e.EventType == "webhook.received" &&
			e.DeduplicationID != nil && *e.DeduplicationID == "evt_123"
	})).Return(nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_123")
		c.Set("webhook_provider", "stripe")
		c.Next()
	})
	wh := NewWebhookHandler(mockRepo)
	r.POST("/webhook", wh.Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook",
		bytes.NewBufferString(`{"event_type":"subscription.created","data":{"subscription_id":"sub_123"}}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockRepo.AssertExpectations(t)
}

func TestHandleWebhook_UnknownEventType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockOutboxRepo)

	r := gin.New()
	wh := NewWebhookHandler(mockRepo)
	r.POST("/webhook", wh.Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook",
		bytes.NewBufferString(`{"event_type":"payment.unknown","data":{"foo":"bar"}}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.JSONEq(t, `{"error":"unknown_event_type"}`, w.Body.String())
	mockRepo.AssertNotCalled(t, "Store")
}

func TestHandleWebhook_MissingField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockOutboxRepo)

	r := gin.New()
	wh := NewWebhookHandler(mockRepo)
	r.POST("/webhook", wh.Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook",
		bytes.NewBufferString(`{"event_type":"subscription.created","data":{}}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"error":"missing_field"}`, w.Body.String())
	mockRepo.AssertNotCalled(t, "Store")
}

func TestHandleWebhook_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockOutboxRepo)

	r := gin.New()
	wh := NewWebhookHandler(mockRepo)
	r.POST("/webhook", wh.Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook", bytes.NewBufferString(`{invalid json}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"error":"invalid_payload"}`, w.Body.String())
	mockRepo.AssertNotCalled(t, "Store")
}

func TestHandleWebhook_StoreError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockOutboxRepo)
	mockRepo.On("Store", mock.Anything, mock.Anything).Return(errors.New("db error"))

	r := gin.New()
	wh := NewWebhookHandler(mockRepo)
	r.POST("/webhook", wh.Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook",
		bytes.NewBufferString(`{"event_type":"subscription.created","data":{"subscription_id":"sub_123"}}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.JSONEq(t, `{"error":"failed_to_store_webhook_event"}`, w.Body.String())
	mockRepo.AssertExpectations(t)
}

func TestHandleWebhook_NoStoreAcksWithoutPersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)

	payload := []byte(`{"event_type":"subscription.created","data":{"subscription_id":"sub_123"}}`)

	r := gin.New()
	wh := NewWebhookHandler()
	r.Use(func(c *gin.Context) {
		c.Set("webhook_raw_body", payload)
		c.Next()
	})
	r.POST("/webhook", wh.Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook", bytes.NewBuffer(payload))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"accepted","event_type":"subscription.created","subscription_id":"sub_123"}`, w.Body.String())
}

func TestHandleWebhook_MissingSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockOutboxRepo)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		// Simulates webhook verification middleware rejecting the request
		sig := c.GetHeader("X-Signature")
		if sig == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing signature"})
			return
		}
		c.Next()
	})
	wh := NewWebhookHandler(mockRepo)
	r.POST("/webhook", wh.Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	mockRepo.AssertNotCalled(t, "Store")
}
