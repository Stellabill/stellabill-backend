package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"stellabill-backend/internal/repository"
)

var planRepo repository.PlanRepository

// SetPlanRepository allows wiring a PlanRepository (used by routes.Register).
func SetPlanRepository(r repository.PlanRepository) {
	planRepo = r
}

func ListPlans(c *gin.Context) {
	// 1. Require planRepo to be set by routes.Register in normal runs. If nil,
	// respond with empty list for backwards compatibility with tests.
	if planRepo == nil {
		c.JSON(http.StatusOK, gin.H{"plans": []Plan{}})
		return
	}

	rows, err := planRepo.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	out := make([]Plan, 0, len(rows))
	for _, r := range rows {
		out = append(out, Plan{
			ID:          r.ID,
			Name:        r.Name,
			Amount:      r.Amount,
			Currency:    r.Currency,
			Interval:    r.Interval,
			Description: r.Description,
		})
	}
	c.JSON(http.StatusOK, gin.H{"plans": out})
}

