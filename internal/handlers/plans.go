package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/pagination"
)

type Plan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Amount      string `json:"amount"` // Changed to string to match tests
	Currency    string `json:"currency"`
	Interval    string `json:"interval"`
	Description string `json:"description"`
}

func (p Plan) GetID() string        { return p.ID }
func (p Plan) GetSortValue() string { return p.Name } // Standardize on Name as sort key

// ListPlans handles requests for listing all available plans.
func (h *Handler) ListPlans(c *gin.Context) {
	limitStr := c.Query("limit")
	limit, err := pagination.ParseLimit(limitStr, 10)
	if err != nil {
		RespondWithValidationError(c, "Invalid pagination limit", map[string]interface{}{
			"reason": err.Error(),
		})
		return
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

	c.JSON(http.StatusOK, gin.H{
		"plans":       page.Items,
		"next_cursor": page.NextCursor,
		"has_more":    page.HasMore,
	})
}
