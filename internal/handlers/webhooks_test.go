package handlers

import (
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

type MockOutboxRepo struct {
	mock.Mock
}

func (m *MockOutboxRepo) Store(event *outbox.Event) error {
	args := m.Called(event)
	return args.Error(0)
}

func (m *MockOutboxRepo) GetPendingEvents(limit int) ([]*outbox.Event, error) { return nil, nil }
func (m *MockOutboxRepo) GetByID(id uuid.UUID) (*outbox.Event, error) { return nil, nil }
func (m *MockOutboxRepo) UpdateStatus(id uuid.UUID, status outbox.Status, errorMessage *string) error { return nil }
func (m *MockOutboxRepo) MarkAsProcessing(id uuid.UUID) error { return nil }
func (m *MockOutboxRepo) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error { return nil }
func (m *MockOutboxRepo) DeleteCompletedEvents(olderThan time.Time) (int64, error) { return 0, nil }
func (m *MockOutboxRepo) ListDeadLetteredEvents(limit int) ([]*outbox.Event, error) { return nil, nil }
func (m *MockOutboxRepo) RequeueEvent(id uuid.UUID) error { return nil }

func TestNewWebhookHandler(t *testing.T) {
	mockRepo := new(MockOutboxRepo)
	handler := NewWebhookHandler(mockRepo)
	assert.NotNil(t, handler)
}

func TestHandleWebhook_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockOutboxRepo)
	mockRepo.On("Store", mock.Anything).Return(nil)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_123")
		c.Set("webhook_provider", "stripe")
		c.Set("webhook_raw_body", []byte(`{"id":"evt_123","type":"payment"}`))
		c.Next()
	})
	r.POST("/webhook", NewWebhookHandler(mockRepo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var response map[string]string
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.Equal(t, "ok", response["status"])
	mockRepo.AssertExpectations(t)
}

func TestHandleWebhook_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockOutboxRepo)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_123")
		c.Set("webhook_provider", "stripe")
		c.Set("webhook_raw_body", []byte(`{invalid json}`)) // causes outbox.NewEventWithDeduplication to fail
		c.Next()
	})
	r.POST("/webhook", NewWebhookHandler(mockRepo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleWebhook_StoreError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockRepo := new(MockOutboxRepo)
	mockRepo.On("Store", mock.Anything).Return(errors.New("db error"))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("webhook_event_id", "evt_123")
		c.Set("webhook_provider", "stripe")
		c.Set("webhook_raw_body", []byte(`{"id":"evt_123","type":"payment"}`))
		c.Next()
	})
	r.POST("/webhook", NewWebhookHandler(mockRepo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	mockRepo.AssertExpectations(t)
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
	r.POST("/webhook", NewWebhookHandler(mockRepo))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/webhook", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
