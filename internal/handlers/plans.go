package handlers

import (
	"encoding/json"
	"net/http"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/requestparams"
	"strconv"

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

// PlanAllowedFields lists the JSON field names that clients may request
// via the ?fields= query parameter on plan endpoints.
var PlanAllowedFields = []string{
	"id",
	"name",
	"amount",
	"currency",
	"interval",
	"description",
}

// ListPlans handles requests for listing all available plans.
// Supports ?fields= for sparse fieldset selection.
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

	// Parse optional ?fields= parameter.
	fields, err := requestparams.ParseFields(c.Query("fields"), PlanAllowedFields)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	setPaginationLinkHeader(c, page, cursorStr != "")

	if fields != nil {
		// Apply sparse fieldset projection.
		projectedItems := make([]map[string]json.RawMessage, 0, len(page.Items))
		for _, item := range page.Items {
			projected, err := ProjectFields(item, fields)
			if err != nil {
				RespondWithInternalError(c, "Failed to build response")
				return
			}
			projectedItems = append(projectedItems, projected)
		}
		c.JSON(http.StatusOK, gin.H{
			"plans":       projectedItems,
			"next_cursor": page.NextCursor,
			"has_more":    page.HasMore,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"plans":       page.Items,
		"next_cursor": page.NextCursor,
		"has_more":    page.HasMore,
	})
}
