package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"stellarbill-backend/internal/repository"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

var planRepository repository.PlanRepository

func SetPlanRepository(repo repository.PlanRepository) {
	planRepository = repo
}

func ListPlans(c *gin.Context) {
	if planRepository == nil {
		c.JSON(http.StatusOK, gin.H{"plans": []interface{}{}})
		return
	}
	plans, err := planRepository.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

type mockPlanRepo struct {
	plans []*repository.PlanRow
}

func (m *mockPlanRepo) List(ctx context.Context) ([]*repository.PlanRow, error) {
	return m.plans, nil
}

func (m *mockPlanRepo) FindByID(ctx context.Context, id string) (*repository.PlanRow, error) {
	for _, p := range m.plans {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (m *mockPlanRepo) FindByIDs(ctx context.Context, ids []string) ([]*repository.PlanRow, error) {
	out := make([]*repository.PlanRow, 0, len(ids))
	for _, id := range ids {
		for _, p := range m.plans {
			if p.ID == id {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func (m *mockPlanRepo) FindByIDsAndTenant(ctx context.Context, ids []string, tenantID string) ([]*repository.PlanRow, error) {
	return m.FindByIDs(ctx, ids)
}

func (m *mockPlanRepo) Update(ctx context.Context, plan *repository.PlanRow, expectedVersion int64) error {
	for i, p := range m.plans {
		if p.ID == plan.ID {
			m.plans[i] = plan
			return nil
		}
	}
	return repository.ErrNotFound
}

func (m *mockPlanRepo) Delete(ctx context.Context, id string, expectedVersion int64) error {
	for i, p := range m.plans {
		if p.ID == id {
			m.plans = append(m.plans[:i], m.plans[i+1:]...)
			return nil
		}
	}
	return repository.ErrNotFound
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
