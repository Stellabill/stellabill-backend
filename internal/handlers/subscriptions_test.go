package handlers

import (
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
)

func TestListSubscriptions(t *testing.T) {
	gin.SetMode(gin.TestMode)

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
