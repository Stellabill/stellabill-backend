package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestListSubscriptions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("success", func(t *testing.T) {
		mockSvc := new(MockSubscriptionService)
		h := &Handler{Subscriptions: mockSvc}

		subs := []Subscription{
			{ID: "sub_1", PlanID: "plan_1", Customer: "cust_1", Status: "active"},
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
	})
}

func TestGetSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("success", func(t *testing.T) {
		mockSvc := new(MockSubscriptionService)
		h := &Handler{Subscriptions: mockSvc}

		sub := &Subscription{ID: "sub_1", PlanID: "plan_1", Status: "active"}
		mockSvc.On("GetSubscription", mock.Anything, "sub_1").Return(sub, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "id", Value: "sub_1"}}

		h.GetSubscription(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var response Subscription
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "sub_1", response.ID)
	})

	t.Run("not found", func(t *testing.T) {
		mockSvc := new(MockSubscriptionService)
		h := &Handler{Subscriptions: mockSvc}

		mockSvc.On("GetSubscription", mock.Anything, "sub_nonexistent").Return(nil, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "id", Value: "sub_nonexistent"}}

		h.GetSubscription(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("missing id", func(t *testing.T) {
		h := &Handler{}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		h.GetSubscription(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("error", func(t *testing.T) {
		mockSvc := new(MockSubscriptionService)
		h := &Handler{Subscriptions: mockSvc}

		mockSvc.On("GetSubscription", mock.Anything, "sub_error").Return(nil, errors.New("db error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = []gin.Param{{Key: "id", Value: "sub_error"}}

		h.GetSubscription(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		var response map[string]string
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "db error", response["error"])
	})
}
