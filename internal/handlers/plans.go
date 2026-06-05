package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/repository"
)

const plansTracerName = "handler/plans"

type Plan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	Interval    string `json:"interval"`
	Description string `json:"description,omitempty"`
}

func (p Plan) GetID() string        { return p.ID }
func (p Plan) GetSortValue() string { return p.Name }

func (h *Handler) ListPlans(c *gin.Context) {
	baseCtx := context.Background()
	if c.Request != nil {
		baseCtx = c.Request.Context()
	}
	ctx, span := otel.Tracer(plansTracerName).Start(baseCtx, "handler.ListPlans")
	defer span.End()
	if c.Request != nil {
		c.Request = c.Request.WithContext(ctx)
	}

	limitStr := c.Query("limit")
	limit, err := pagination.ParseLimit(limitStr, 10)
	if err != nil {
		RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, "Invalid pagination limit", map[string]interface{}{
			"reason": err.Error(),
		})
		return
	}

	cursorStr := c.Query("cursor")
	cursor, err := pagination.Decode(cursorStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid cursor format"})
		return
	}

	plans, err := h.Plans.ListPlans(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load plans"})
		return
	}

	if plans == nil {
		plans = []Plan{}
	}

	page := pagination.PaginateSlice(plans, cursor, limit)

	c.JSON(http.StatusOK, gin.H{
		"plans":       page.Items,
		"next_cursor": page.NextCursor,
		"has_more":    page.HasMore,
	})
}

var planRepo repository.PlanRepository

// SetPlanRepository allows wiring a PlanRepository (used by routes.Register).
func SetPlanRepository(r repository.PlanRepository) {
	planRepo = r
}

func ListPlans(c *gin.Context) {
	baseCtx := context.Background()
	if c.Request != nil {
		baseCtx = c.Request.Context()
	}
	ctx, span := otel.Tracer(plansTracerName).Start(baseCtx, "handler.ListPlans")
	defer span.End()
	if c.Request != nil {
		c.Request = c.Request.WithContext(ctx)
	}

	if planRepo == nil {
		c.JSON(http.StatusOK, gin.H{"plans": []Plan{}})
		return
	}

	rows, err := planRepo.List(ctx)
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
