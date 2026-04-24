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
}
