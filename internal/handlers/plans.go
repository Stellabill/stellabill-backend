package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

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
	if h.Plans == nil {
		RespondWithError(c, http.StatusServiceUnavailable, ErrorCodeServiceUnavailable, "plan service is unavailable")
		return
	}

	baseCtx := context.Background()
	if c.Request != nil {
		baseCtx = c.Request.Context()
	}
	ctx, span := otel.Tracer(plansTracerName).Start(baseCtx, "handler.ListPlans")
	defer span.End()
	if c.Request != nil {
		c.Request = c.Request.WithContext(ctx)
	}

	if h.Plans == nil {
		RespondWithError(c, http.StatusServiceUnavailable, ErrorCodeServiceUnavailable, "plan service is unavailable")
		return
	}

	limitStr := c.Query("limit")
	if limitStr != "" {
		if rawLimit, err := strconv.Atoi(limitStr); err == nil && rawLimit > 100 {
			RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, "Limit exceeds maximum of 100", map[string]interface{}{
				"reason": "limit cannot be greater than 100",
			})
			return
		}
	}

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
		"plans": page.Items,
		"pagination": gin.H{
			"next_cursor": page.NextCursor,
			"has_more":    page.HasMore,
		},
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

func GetPlan(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	if planRepo == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "plan not found"})
		return
	}

	row, err := planRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "plan not found"})
		return
	}

	plan := Plan{
		ID:          row.ID,
		Name:        row.Name,
		Amount:      row.Amount,
		Currency:    row.Currency,
		Interval:    row.Interval,
		Description: row.Description,
	}

	tenantID := c.GetString("tenantID")
	versionBytes, _ := json.Marshal(plan)
	version := fmt.Sprintf("%x", sha256.Sum256(versionBytes))
	eTagHash := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s", tenantID, plan.ID, version)))
	eTag := fmt.Sprintf(`"%x"`, eTagHash)

	c.Header("ETag", eTag)
	c.Header("Cache-Control", "private, max-age=0, must-revalidate")

	if match := c.GetHeader("If-None-Match"); match == eTag {
		c.Status(http.StatusNotModified)
		return
	}

	c.JSON(http.StatusOK, plan)
}
