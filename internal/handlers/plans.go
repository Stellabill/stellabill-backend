package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/jsonx"
	"stellarbill-backend/internal/pagination"
)

type Plan struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Amount      string    `json:"amount"` // Changed to string to match tests
	Currency    string    `json:"currency"`
	Interval    string    `json:"interval"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"-"`
	Version     int64     `json:"-"`
	ETag        string    `json:"etag"`
}

func (h *Handler) PatchPlan(c *gin.Context) {
	id := c.Param("id")
	expectedVersion, err := EnsureIfMatch(c)
	if err != nil {
		return
	}

	var req Plan
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.Plans.PatchPlan(c, id, &req, expectedVersion); err != nil {
		if err.Error() == "concurrent update" {
			c.JSON(http.StatusPreconditionFailed, gin.H{"error": "precondition failed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (h *Handler) DeletePlan(c *gin.Context) {
	id := c.Param("id")
	expectedVersion, err := EnsureIfMatch(c)
	if err != nil {
		return
	}

	if err := h.Plans.DeletePlan(c, id, expectedVersion); err != nil {
		if err.Error() == "concurrent update" {
			c.JSON(http.StatusPreconditionFailed, gin.H{"error": "precondition failed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

func (p Plan) GetID() string        { return p.ID }
func (p Plan) GetSortValue() string { return p.Name } // Standardize on Name as sort key

// ListPlans handles requests for listing all available plans.
func (h *Handler) ListPlans(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}

	cursorStr := c.Query("cursor")
	cursor, err := pagination.Decode(cursorStr)
	if err != nil {
		RespondWithInternalError(c, "Failed to retrieve plans")
		return
	}

	// Fetch plans from the service/repository
	allPlans, err := h.Plans.ListPlans(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load plans"})
		return
	}

	// Paginate the slice. In a real DB repo, this would be in the query.
	page := pagination.PaginateSlice(allPlans, cursor, limit)

	// Use jsonx.GinRenderer (sonic on amd64/arm64 with -tags=sonic,
	// encoding/json elsewhere) to reduce per-request serialisation CPU
	// on this high-QPS list endpoint. See internal/jsonx for details.
	c.Render(http.StatusOK, jsonx.GinRenderer{Data: gin.H{
		"plans":       page.Items,
		"next_cursor": page.NextCursor,
		"has_more":    page.HasMore,
	}})
}
