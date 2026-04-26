package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/requestparams"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/subscriptions"
)

type Subscription struct {
	ID          string `json:"id"`
	PlanID      string `json:"plan_id"`
	Customer    string `json:"customer"`
	Status      string `json:"status"`
	Amount      string `json:"amount"`
	Interval    string `json:"interval"`
	NextBilling string `json:"next_billing,omitempty"`
}

func (s Subscription) GetID() string        { return s.ID }
func (s Subscription) GetSortValue() string { return s.Customer } // Sort by customer for now

func (h *Handler) ListSubscriptions(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}

	cursorStr := c.Query("cursor")
	cursor, err := pagination.Decode(cursorStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor format"})
		return
	}

	allSubs, err := h.Subscriptions.ListSubscriptions(c)
	if err != nil {
		RespondWithInternalError(c, "Failed to retrieve subscriptions")
		return
	}

	page := pagination.PaginateSlice(allSubs, cursor, limit)

	c.JSON(http.StatusOK, gin.H{
		"subscriptions": page.Items,
		"next_cursor":   page.NextCursor,
		"has_more":      page.HasMore,
	})
}


func (h *Handler) GetSubscription(c *gin.Context) {
	id := c.Param("id")
	c.JSON(http.StatusOK, Subscription{
		ID:       id,
		PlanID:   "plan_placeholder",
		Customer: "customer_placeholder",
		Status:   "placeholder",
		Amount:   "0",
		Interval: "monthly",
	})
}

// NewGetSubscriptionHandler returns a gin.HandlerFunc that retrieves a full
// subscription detail using the provided SubscriptionService.
func NewGetSubscriptionHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Minimal, safe handler that validates caller and path, then delegates to the service.
		callerID, exists := c.Get("callerID")
		if !exists {
			RespondWithAuthError(c, "Missing authentication credentials")
			return
		}

		tenantID, exists := c.Get("tenantID")
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tenant id required"})
			return
		}

		if _, err := requestparams.SanitizeQuery(c.Request.URL.Query(), requestparams.QueryRules{}); err != nil {
			RespondWithValidationError(c, "Invalid query parameters", map[string]interface{}{
				"reason": err.Error(),
			})
			return
		}

		id, err := requestparams.NormalizePathID("id", c.Param("id"))
		if err != nil {
			RespondWithValidationError(c, "Invalid subscription id", map[string]interface{}{
				"field":  "id",
				"reason": err.Error(),
			})
			return
		}

		// Delegate to service
		_, _, err = svc.GetDetail(c.Request.Context(), tenantID.(string), callerID.(string), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"id": id})
	}
}

// UpdateSubscriptionStatus handles status updates with validation
func UpdateSubscriptionStatus(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		RespondWithValidationError(c, "subscription id is required", map[string]interface{}{
			"field":  "id",
			"reason": "cannot be empty",
		})
		return
	}

	var payload struct {
		Status string `json:"status" binding:"required"`
	}

	if err := c.ShouldBindJSON(&payload); err != nil {
		RespondWithValidationError(c, "Invalid request body", map[string]interface{}{
			"field":  "status",
			"reason": err.Error(),
		})
		return
	}

	// TODO: fetch current subscription from DB
	currentStatus := "active" // placeholder

	if err := subscriptions.CanTransition(currentStatus, payload.Status); err != nil {
		RespondWithErrorDetails(c, http.StatusConflict, ErrorCodeConflict, "Invalid status transition", map[string]interface{}{
			"current_status": currentStatus,
			"requested_status": payload.Status,
			"reason": err.Error(),
		})
		return
	}

	// TODO: persist update

	c.JSON(http.StatusOK, gin.H{
		"id":     id,
		"status": payload.Status,
	})
}