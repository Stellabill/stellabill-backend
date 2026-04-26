package handlers

import (
	"net/http"
	"strconv"

	"stellarbill-backend/internal/pagination"

	"github.com/gin-gonic/gin"
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

	allPlans, err := h.Plans.ListPlans(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load plans"})
		return
	}

	// For now, we paginate the slice. In a real DB repo, this would be in the query.
	page := pagination.PaginateSlice(allPlans, cursor, limit)

	c.JSON(http.StatusOK, gin.H{
		"plans":       page.Items,
		"next_cursor": page.NextCursor,
		"has_more":    page.HasMore,
	})
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
