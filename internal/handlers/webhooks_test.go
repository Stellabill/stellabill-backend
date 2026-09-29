package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"stellarbill-backend/internal/outbox"
)

// MockOutboxRepo satisfies outbox.Repository for webhook handler tests.
type MockOutboxRepo struct {
	mock.Mock
}

func (m *MockOutboxRepo) Store(ctx context.Context, event *outbox.Event) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockOutboxRepo) GetPendingEvents(limit int) ([]*outbox.Event, error) {
	args := m.Called(limit)
	return args.Get(0).([]*outbox.Event), args.Error(1)
}

func (m *MockOutboxRepo) GetByID(id uuid.UUID) (*outbox.Event, error) {
	args := m.Called(id)
	return args.Get(0).(*outbox.Event), args.Error(1)
}

func (m *MockOutboxRepo) UpdateStatus(id uuid.UUID, status outbox.Status, errorMessage *string) error {
	args := m.Called(id, status, errorMessage)
	return args.Error(0)
}

func (m *MockOutboxRepo) MarkAsProcessing(id uuid.UUID) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockOutboxRepo) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error {
	args := m.Called(id, nextRetryAt, errorMessage)
	return args.Error(0)
}

func (m *MockOutboxRepo) DeleteCompletedEvents(olderThan time.Time) (int64, error) {
	args := m.Called(olderThan)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockOutboxRepo) ListDeadLetteredEvents(limit int) ([]*outbox.Event, error) {
	args := m.Called(limit)
	return args.Get(0).([]*outbox.Event), args.Error(1)
}

func (m *MockOutboxRepo) RequeueEvent(id uuid.UUID) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockOutboxRepo) EnsurePublisherProgressTable() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockOutboxRepo) GetPublisherProgress(publisher string) (*uuid.UUID, error) {
	args := m.Called(publisher)
	return args.Get(0).(*uuid.UUID), args.Error(1)
}

func (m *MockOutboxRepo) GetPendingEventsForPublisher(publisher string, limit int) ([]*outbox.Event, error) {
	args := m.Called(publisher, limit)
	return args.Get(0).([]*outbox.Event), args.Error(1)
}

func (m *MockOutboxRepo) MarkPublished(publisher string, event *outbox.Event, publishers []string) error {
	args := m.Called(publisher, event, publishers)
	return args.Error(0)
}

func TestNewWebhookHandler(t *testing.T) {
	mockRepo := new(MockOutboxRepo)
	h := NewWebhookHandler(mockRepo)
	assert.NotNil(t, h)
	assert.NotNil(t, h.replayGuard)
}

func TestWebhookHandler_Receive_Success_HeaderEventID(t *testing.T) {
	mockRepo := new(MockOutboxRepo)
	mockRepo.On("Store", mock.Anything, mock.Anything).Return(nil)

	r := gin.New()
	r.POST("/api/webhooks/test", func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_header_1")
		c.Set("webhook_provider", "generic")
		c.Set("webhook_raw_body", []byte(`{"type":"subscription.created","data":{}}`))
		c.Next()
	}, NewWebhookHandler(mockRepo).Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/webhooks/test", bytes.NewBufferString(`{"type":"subscription.created","data":{}}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "accepted", resp["status"])
	assert.Equal(t, "subscription.created", resp["event_type"])

	storeArgs := mockRepo.Calls[0].Arguments
	stored := storeArgs.Get(1).(*outbox.Event)
	assert.NotNil(t, stored.DeduplicationID)
	assert.Equal(t, "evt_header_1", *stored.DeduplicationID)
	mockRepo.AssertExpectations(t)
}

func TestWebhookHandler_Receive_StripeEventIDFromPayload(t *testing.T) {
	mockRepo := new(MockOutboxRepo)
	mockRepo.On("Store", mock.Anything, mock.Anything).Return(nil)

	r := gin.New()
	r.POST("/api/webhooks/test", func(c *gin.Context) {
		// Stripe sends no event ID header; the verification middleware does not
		// set webhook_event_id. The handler must derive it from the payload.
		c.Set("webhook_provider", "stripe")
		c.Set("webhook_raw_body", []byte(`{"id":"evt_123","object":"event","type":"payment_intent.succeeded","data":{"object":{}}}`))
		c.Next()
	}, NewWebhookHandler(mockRepo).Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/webhooks/test", bytes.NewBufferString(`{"id":"evt_123","type":"payment_intent.succeeded"}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)

	storeArgs := mockRepo.Calls[0].Arguments
	stored := storeArgs.Get(1).(*outbox.Event)
	assert.NotNil(t, stored.DeduplicationID)
	assert.Equal(t, "evt_123", *stored.DeduplicationID)
	mockRepo.AssertExpectations(t)
}

func TestWebhookHandler_Receive_ReplayDeduped(t *testing.T) {
	mockRepo := new(MockOutboxRepo)
	mockRepo.On("Store", mock.Anything, mock.Anything).Return(nil)

	r := gin.New()
	r.POST("/api/webhooks/test", func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_dup_1")
		c.Set("webhook_provider", "generic")
		c.Set("webhook_raw_body", []byte(`{"type":"invoice.paid"}`))
		c.Next()
	}, NewWebhookHandler(mockRepo).Receive)

	// First delivery persists.
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodPost, "/api/webhooks/test", bytes.NewBufferString(`{"type":"invoice.paid"}`))
	r.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusAccepted, w1.Code)

	// Replay with the same event ID is rejected by the in-memory cache: no
	// second persistence, 200 idempotent ack.
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost, "/api/webhooks/test", bytes.NewBufferString(`{"type":"invoice.paid"}`))
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["deduplicated"])

	mockRepo.AssertNumberOfCalls(t, "Store", 1)
}

func TestWebhookHandler_Receive_MissingTrackingIdentifiers(t *testing.T) {
	mockRepo := new(MockOutboxRepo)

	r := gin.New()
	r.POST("/api/webhooks/test", func(c *gin.Context) {
		c.Set("webhook_provider", "generic")
		c.Set("webhook_raw_body", []byte(`{"type":"x","id":""}`))
		c.Next()
	}, NewWebhookHandler(mockRepo).Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/webhooks/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "missing_tracking_identifiers", resp["error"])
	mockRepo.AssertNotCalled(t, "Store", mock.Anything, mock.Anything)
}

func TestWebhookHandler_Receive_MissingProvider(t *testing.T) {
	mockRepo := new(MockOutboxRepo)

	r := gin.New()
	r.POST("/api/webhooks/test", func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_1")
		c.Set("webhook_raw_body", []byte(`{"type":"x"}`))
		c.Next()
	}, NewWebhookHandler(mockRepo).Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/webhooks/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	mockRepo.AssertNotCalled(t, "Store", mock.Anything, mock.Anything)
}

func TestWebhookHandler_Receive_InvalidJSON(t *testing.T) {
	mockRepo := new(MockOutboxRepo)

	r := gin.New()
	r.POST("/api/webhooks/test", func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_1")
		c.Set("webhook_provider", "generic")
		c.Set("webhook_raw_body", []byte(`{invalid`))
		c.Next()
	}, NewWebhookHandler(mockRepo).Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/webhooks/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	mockRepo.AssertNotCalled(t, "Store", mock.Anything, mock.Anything)
}

func TestWebhookHandler_Receive_StoreErrorAcksIdempotent(t *testing.T) {
	mockRepo := new(MockOutboxRepo)
	mockRepo.On("Store", mock.Anything, mock.Anything).Return(errors.New("unique_violation"))

	r := gin.New()
	r.POST("/api/webhooks/test", func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_err_1")
		c.Set("webhook_provider", "generic")
		c.Set("webhook_raw_body", []byte(`{"type":"x"}`))
		c.Next()
	}, NewWebhookHandler(mockRepo).Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/webhooks/test", nil)
	r.ServeHTTP(w, req)

	// A store error (e.g. the UNIQUE index on deduplication_id) is an
	// idempotent ack: the event is already recorded and must not be reprocessed.
	assert.Equal(t, http.StatusOK, w.Code)
	mockRepo.AssertNumberOfCalls(t, "Store", 1)
}

func TestWebhookHandler_Receive_NilStoreReturns503(t *testing.T) {
	r := gin.New()
	r.POST("/api/webhooks/test", func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_1")
		c.Set("webhook_provider", "generic")
		c.Set("webhook_raw_body", []byte(`{"type":"x"}`))
		c.Next()
	}, NewWebhookHandler(nil).Receive)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/webhooks/test", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "outbox_unavailable", resp["error"])
}
