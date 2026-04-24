package handlers

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/repository"
)

var planRepo repository.PlanRepository

// SetPlanRepository allows wiring a PlanRepository (used by routes.Register).
func SetPlanRepository(r repository.PlanRepository) {
	planRepo = r
}

// Plan is the JSON shape returned by list/get plan endpoints.
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if plans == nil {
		plans = []Plan{}
	}
	// Stable sort by ID guarantees deterministic ordering for pagination.
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].ID < plans[j].ID })
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// ListPlans is the standalone handler used when a PlanRepository is wired directly.
func ListPlans(c *gin.Context) {
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
	// Stable sort by ID guarantees deterministic ordering for pagination.
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	c.JSON(http.StatusOK, gin.H{"plans": out})
}
