package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/repositories"
)

type Plan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	Interval    string `json:"interval"`
	Description string `json:"description,omitempty"`
}

func (h *Handler) ListPlans(c *gin.Context) {
	plans, err := h.Plans.ListPlans(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load plans"})
		return
	}

	if plans == nil {
		plans = []Plan{}
	}

	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

var planRepo repositories.PlanRepository

// SetPlanRepository allows wiring a PlanRepository (used by routes.Register).
func SetPlanRepository(r repositories.PlanRepository) {
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
		p := Plan{
			ID:       r.ID,
			Name:     r.Name,
			Amount:   r.Amount,
			Currency: r.Currency,
			Interval: r.Interval,
		}
		if r.Description != nil {
			p.Description = *r.Description
		}
		out = append(out, p)
	}
	c.JSON(http.StatusOK, gin.H{"plans": out})
}
