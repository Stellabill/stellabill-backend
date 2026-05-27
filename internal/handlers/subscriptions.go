package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/service"
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
	sub, err := h.Subscriptions.GetSubscription(c, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, sub)
}

// NewGetSubscriptionHandler returns a gin.HandlerFunc that retrieves a full
// subscription detail using the provided SubscriptionService. It requires
// callerID and tenantID to already be present on the gin context.
func NewGetSubscriptionHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			RespondWithInternalError(c, "Subscription service is unavailable")
			return
		}

		callerID, ok := getRequiredStringContextValue(c, "callerID", "Missing authentication credentials")
		if !ok {
			return
		}

		tenantID, ok := getRequiredStringContextValue(c, "tenantID", "Missing tenant context")
		if !ok {
			return
		}

		detail, warnings, err := svc.GetDetail(c.Request.Context(), tenantID, callerID, c.Param("id"))
		if err != nil {
			status, code, message := MapServiceErrorToResponse(err)
			RespondWithError(c, status, code, message)
			return
		}

		c.JSON(http.StatusOK, service.ResponseEnvelope{
			APIVersion: "v1",
			Data:       detail,
			Warnings:   warnings,
		})
	}
}

// getRequiredStringContextValue extracts a string from gin context and rejects
// missing or malformed authentication scope to avoid accidental broad queries.
func getRequiredStringContextValue(c *gin.Context, key string, missingMessage string) (string, bool) {
	value, exists := c.Get(key)
	if !exists {
		RespondWithAuthError(c, missingMessage)
		return "", false
	}

	str, ok := value.(string)
	if !ok || str == "" {
		RespondWithAuthError(c, missingMessage)
		return "", false
	}

	return str, true
}
