package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"stellarbill-backend/internal/outbox"
)

func TestNewHandler(t *testing.T) {
	mockPlans := new(MockPlanService)
	mockSubs := new(MockSubscriptionService)
	
	h := NewHandler(mockPlans, mockSubs, nil, nil)
	
	assert.NotNil(t, h)
	assert.Equal(t, mockPlans, h.Plans)
	assert.Equal(t, mockSubs, h.Subscriptions)
}

func TestNewHandlerWithDependencies(t *testing.T) {
	mockPlans := new(MockPlanService)
	mockSubs := new(MockSubscriptionService)
	
	h := NewHandlerWithDependencies(mockPlans, mockSubs, "db", "outbox")
	
	assert.NotNil(t, h)
	assert.Equal(t, mockPlans, h.Plans)
	assert.Equal(t, mockSubs, h.Subscriptions)
	assert.Equal(t, "db", h.Database)
	assert.Equal(t, "outbox", h.Outbox)
}

type mockOutboxRepo struct {
	events []*outbox.Event
	err    error
	requeueErr error
}

func (m *mockOutboxRepo) Store(event *outbox.Event) error { return nil }
func (m *mockOutboxRepo) GetPendingEvents(limit int) ([]*outbox.Event, error) { return nil, nil }
func (m *mockOutboxRepo) GetByID(id uuid.UUID) (*outbox.Event, error) { return nil, nil }
func (m *mockOutboxRepo) UpdateStatus(id uuid.UUID, status outbox.Status, errorMessage *string) error { return nil }
func (m *mockOutboxRepo) MarkAsProcessing(id uuid.UUID) error { return nil }
func (m *mockOutboxRepo) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error { return nil }
func (m *mockOutboxRepo) DeleteCompletedEvents(before time.Time) (int64, error) { return 0, nil }

func (m *mockOutboxRepo) ListDeadLetteredEvents(limit int) ([]*outbox.Event, error) {
	return m.events, m.err
}

func (m *mockOutboxRepo) RequeueEvent(id uuid.UUID) error {
	return m.requeueErr
}

func TestListDeadLetteredEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	t.Run("success", func(t *testing.T) {
		repo := &mockOutboxRepo{events: []*outbox.Event{}}
		h := &Handler{OutboxRepo: repo}
		
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/?limit=10", nil)
		
		h.ListDeadLetteredEvents(c)
		
		assert.Equal(t, http.StatusOK, w.Code)
	})
	
	t.Run("nil repo", func(t *testing.T) {
		h := &Handler{}
		
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		
		h.ListDeadLetteredEvents(c)
		
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("repo error", func(t *testing.T) {
		repo := &mockOutboxRepo{err: errors.New("db error")}
		h := &Handler{OutboxRepo: repo}
		
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
		
		h.ListDeadLetteredEvents(c)
		
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestRequeueOutboxEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	t.Run("success", func(t *testing.T) {
		repo := &mockOutboxRepo{}
		h := &Handler{OutboxRepo: repo}
		
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/", nil)
		c.Params = []gin.Param{{Key: "id", Value: uuid.New().String()}}
		
		h.RequeueOutboxEvent(c)
		
		assert.Equal(t, http.StatusNoContent, c.Writer.Status())
	})
	
	t.Run("nil repo", func(t *testing.T) {
		h := &Handler{}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		h.RequeueOutboxEvent(c)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
	
	t.Run("invalid id", func(t *testing.T) {
		repo := &mockOutboxRepo{}
		h := &Handler{OutboxRepo: repo}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "id", Value: "not-a-uuid"}}
		
		h.RequeueOutboxEvent(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
	
	t.Run("event not found", func(t *testing.T) {
		repo := &mockOutboxRepo{requeueErr: errors.New("event not found or not in failed status")}
		h := &Handler{OutboxRepo: repo}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "id", Value: uuid.New().String()}}
		
		h.RequeueOutboxEvent(c)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
	
	t.Run("other error", func(t *testing.T) {
		repo := &mockOutboxRepo{requeueErr: errors.New("some error")}
		h := &Handler{OutboxRepo: repo}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "id", Value: uuid.New().String()}}
		
		h.RequeueOutboxEvent(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
