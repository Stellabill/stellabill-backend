package handlers

import (
	"context"
	"net/http"
	"strconv"

	"stellarbill-backend/internal/pagination"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/repository"
)

func (h *Handler) ListPlans(c *gin.Context) {
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}

	plans, err := h.planService.ListPlans(ctx)
	if err != nil {
		RespondWithInternalError(c, "Failed to retrieve plans")
		return
	}

	if plans == nil {
		plans = []services.Plan{}
	}

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
		RespondWithInternalError(c, "Failed to retrieve plans")
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
