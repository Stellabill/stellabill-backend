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

func TestListPlans(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("success", func(t *testing.T) {
		mockSvc := new(MockPlanService)
		h := &Handler{Plans: mockSvc}

		plans := []Plan{
			{ID: "plan_1", Name: "Basic", Amount: "10.00", Currency: "USD", Interval: "month"},
		}
		mockSvc.On("ListPlans", mock.Anything).Return(plans, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		h.ListPlans(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string][]Plan
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Len(t, response["plans"], 1)
		assert.Equal(t, "plan_1", response["plans"][0].ID)
	})

	t.Run("error", func(t *testing.T) {
		mockSvc := new(MockPlanService)
		h := &Handler{Plans: mockSvc}

		mockSvc.On("ListPlans", mock.Anything).Return(nil, errors.New("db error"))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		h.ListPlans(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		var response map[string]string
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.Equal(t, "failed to load plans", response["error"])
	})

	t.Run("nil dependency returns 503 instead of panicking", func(t *testing.T) {
		h := &Handler{} // Plans deliberately left nil

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/plans", nil)

		assert.NotPanics(t, func() { h.ListPlans(c) })

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		var response ErrorEnvelope
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "SERVICE_UNAVAILABLE", response.Code)
		assert.Contains(t, response.Message, "plan service is unavailable")
	})

	t.Run("empty list", func(t *testing.T) {
		mockSvc := new(MockPlanService)
		h := &Handler{Plans: mockSvc}

		mockSvc.On("ListPlans", mock.Anything).Return([]Plan{}, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/plans", nil)

		h.ListPlans(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Empty(t, response["plans"])
		assert.Equal(t, false, response["has_more"])
	})

	t.Run("invalid limits", func(t *testing.T) {
		invalidInputs := []string{"abc", "1abc", " ", "  "}
		for _, input := range invalidInputs {
			t.Run(input, func(t *testing.T) {
				mockSvc := new(MockPlanService)
				h := &Handler{Plans: mockSvc}

				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("GET", "/plans?limit="+url.QueryEscape(input), nil)

				h.ListPlans(c)

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
				mockSvc := new(MockPlanService)
				h := &Handler{Plans: mockSvc}

				// Create 105 mock plans to verify pagination slicing limit
				var plans []Plan
				for i := 1; i <= 105; i++ {
					plans = append(plans, Plan{
						ID:   "plan_" + strconv.Itoa(i),
						Name: "Plan " + strconv.Itoa(i),
					})
				}
				mockSvc.On("ListPlans", mock.Anything).Return(plans, nil)

				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("GET", "/plans?limit="+url.QueryEscape(tc.limitStr), nil)

				h.ListPlans(c)

				assert.Equal(t, http.StatusOK, w.Code)
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)

				items := response["plans"].([]interface{})
				assert.Len(t, items, tc.expectedLimit)
			})
		}
	})
}

