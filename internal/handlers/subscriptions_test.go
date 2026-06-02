package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"stellarbill-backend/internal/service"
)

// mockSubscriptionService is a test double for service.SubscriptionService.
type mockSubscriptionService struct {
	detail   *service.SubscriptionDetail
	warnings []string
	err      error
	callerID string
	id       string
}

func (m *mockSubscriptionService) GetDetail(_ context.Context, tenantID, callerID, id string) (*service.SubscriptionDetail, []string, error) {
	m.callerID = callerID
	m.id = id
	return m.detail, m.warnings, m.err
}

func (m *mockSubscriptionService) ChangeStatus(ctx context.Context, tenantID string, actorID string, subscriptionID string, targetStatus string) (*service.SubscriptionStatusChange, error) {
	return nil, nil
}

func (m *mockSubscriptionService) ListSubscriptions(c *gin.Context) ([]Subscription, error) {
	return nil, nil
}

func (m *mockSubscriptionService) GetSubscription(c *gin.Context, id string) (*Subscription, error) {
	return nil, nil
}

// setupRouter builds a minimal Gin router with the Handler wired up.
// If setCallerID is true, a middleware injects "callerID" into the context.
func setupRouter(svc *mockSubscriptionService, setCallerID bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if setCallerID {
		r.Use(func(c *gin.Context) {
			c.Set("callerID", "caller-123")
			c.Set("tenantID", "tenant-1")
			c.Next()
		})
	}
	r.GET("/api/subscriptions/:id", NewGetSubscriptionHandler(svc))
	return r
}

func TestHandler_ListSubscriptions(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockSvc := new(MockSubscriptionService)
		h := &Handler{Subscriptions: mockSvc}

		subs := []Subscription{
			{ID: "sub_1", PlanID: "plan_1", Customer: "Alice", Status: "active"},
		}
		mockSvc.On("ListSubscriptions", mock.Anything).Return(subs, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		h.ListSubscriptions(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string][]Subscription
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Len(t, response["subscriptions"], 1)
		assert.Equal(t, "sub_1", response["subscriptions"][0].ID)
	})

	t.Run("error", func(t *testing.T) {
		mockSvc := new(MockSubscriptionService)
		h := &Handler{Subscriptions: mockSvc}

		mockSvc.On("ListSubscriptions", mock.Anything).Return(nil, errors.New("db error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		h.ListSubscriptions(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		var response ErrorEnvelope
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "INTERNAL_ERROR", response.Code)
		assert.Contains(t, response.Message, "Failed to retrieve subscription")
	})

	t.Run("nil dependency returns 503 instead of panicking", func(t *testing.T) {
		h := &Handler{} // Subscriptions deliberately left nil

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/subscriptions", nil)

		assert.NotPanics(t, func() { h.ListSubscriptions(c) })

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		var response ErrorEnvelope
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "SERVICE_UNAVAILABLE", response.Code)
		assert.Contains(t, response.Message, "subscription service is unavailable")
	})

	t.Run("empty list", func(t *testing.T) {
		mockSvc := new(MockSubscriptionService)
		h := &Handler{Subscriptions: mockSvc}

		mockSvc.On("ListSubscriptions", mock.Anything).Return([]Subscription{}, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/subscriptions", nil)

		h.ListSubscriptions(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Empty(t, response["subscriptions"])
		assert.Equal(t, false, response["has_more"])
	})

	t.Run("invalid limits", func(t *testing.T) {
		invalidInputs := []string{"abc", "1abc", " ", "  "}
		for _, input := range invalidInputs {
			t.Run(input, func(t *testing.T) {
				mockSvc := new(MockSubscriptionService)
				h := &Handler{Subscriptions: mockSvc}

				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("GET", "/subscriptions?limit="+url.QueryEscape(input), nil)

				h.ListSubscriptions(c)

				assert.Equal(t, http.StatusBadRequest, w.Code)
				var response ErrorEnvelope
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Equal(t, "VALIDATION_FAILED", response.Code)
				assert.Contains(t, response.Message, "Invalid pagination limit")
			})
		}
	})

	t.Run("clamped and valid limits", func(t *testing.T) {
		validInputs := []struct {
			limitStr      string
			expectedLimit int
		}{
			{"1", 1},
			{"20", 20},
			{"100", 100},
			{"101", 100},
			{"100000", 100},
			{"0", 10},
			{"-10", 10},
			{"", 10},
		}

		for _, tc := range validInputs {
			t.Run(tc.limitStr, func(t *testing.T) {
				mockSvc := new(MockSubscriptionService)
				h := &Handler{Subscriptions: mockSvc}

				// Create 105 mock subscriptions to verify pagination slicing limit
				var subs []Subscription
				for i := 1; i <= 105; i++ {
					subs = append(subs, Subscription{
						ID:       "sub_" + strconv.Itoa(i),
						Customer: "Customer " + strconv.Itoa(i),
					})
				}
				mockSvc.On("ListSubscriptions", mock.Anything).Return(subs, nil)

				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("GET", "/subscriptions?limit="+url.QueryEscape(tc.limitStr), nil)

				h.ListSubscriptions(c)

				assert.Equal(t, http.StatusOK, w.Code)
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)

				items := response["subscriptions"].([]interface{})
				assert.Len(t, items, tc.expectedLimit)
			})
		}
	})
}
