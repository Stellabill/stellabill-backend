package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"stellarbill-backend/internal/repository"
)

type mockPlanRepo struct {
	plans []*repository.PlanRow
}
func (m *mockPlanRepo) List(ctx context.Context) ([]*repository.PlanRow, error) {
	return m.plans, nil
}
func (m *mockPlanRepo) FindByID(ctx context.Context, id string) (*repository.PlanRow, error) {
	return nil, nil
}

func TestStandaloneListPlans(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	t.Run("nil repo", func(t *testing.T) {
		SetPlanRepository(nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		ListPlans(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"plans":[]`)
	})

	t.Run("with repo", func(t *testing.T) {
		repo := &mockPlanRepo{plans: []*repository.PlanRow{{ID: "123", Name: "Basic"}}}
		SetPlanRepository(repo)
		
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		// Set dummy request for context
		c.Request, _ = http.NewRequest(http.MethodGet, "/", nil)
		ListPlans(c)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Basic")
	})
}
