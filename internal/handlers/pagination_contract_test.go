package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"stellarbill-backend/internal/pagination"
)

func TestPaginationContract(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Plans pagination", func(t *testing.T) {
		mockSvc := new(MockPlanService)
		h := &Handler{Plans: mockSvc}

		plans := make([]Plan, 15)
		for i := 0; i < 15; i++ {
			plans[i] = Plan{
				ID:     "plan_" + string(rune('a'+i)),
				Name:   "Plan " + string(rune('A'+i)),
				Amount: "1000",
			}
		}
		mockSvc.On("ListPlans", mock.Anything).Return(plans, nil)

		// Request page 1
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/plans?limit=5", nil)

		h.ListPlans(c)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)

		assert.Len(t, resp["plans"], 5)
		assert.True(t, resp["has_more"].(bool))
		assert.NotEmpty(t, resp["next_cursor"])

		nextCursor := resp["next_cursor"].(string)

		// Request page 2
		w2 := httptest.NewRecorder()
		c2, _ := gin.CreateTestContext(w2)
		c2.Request = httptest.NewRequest(http.MethodGet, "/api/plans?limit=5&cursor="+nextCursor, nil)

		h.ListPlans(c2)

		var resp2 map[string]interface{}
		json.Unmarshal(w2.Body.Bytes(), &resp2)
		assert.Len(t, resp2["plans"], 5)
		
		// Verify item continuity
		firstItemPage2 := resp2["plans"].([]interface{})[0].(map[string]interface{})
		lastItemPage1 := resp["plans"].([]interface{})[4].(map[string]interface{})
		assert.NotEqual(t, lastItemPage1["id"], firstItemPage2["id"])
	})

	t.Run("Invalid cursor", func(t *testing.T) {
		mockSvc := new(MockPlanService)
		h := &Handler{Plans: mockSvc}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/plans?cursor=invalid", nil)

		h.ListPlans(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp map[string]string
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "invalid cursor format", resp["error"])
	})
}
