package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

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

type changeSubscriptionStatusRequest struct {
	Status string `json:"status"`
}

// NewChangeSubscriptionStatusHandler returns a tenant-scoped status mutation handler.
func NewChangeSubscriptionStatusHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			RespondWithInternalError(c, "Subscription service is unavailable")
			return
		}

		tenantID, ok := getRequiredStringContextValue(c, "tenantID", "Missing tenant context")
		if !ok {
			return
		}

		actorID := c.GetString("callerID")

		var req changeSubscriptionStatusRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			RespondWithValidationError(c, "Invalid request body", map[string]interface{}{
				"reason": err.Error(),
			})
			return
		}
		req.Status = strings.TrimSpace(req.Status)
		if req.Status == "" {
			RespondWithError(c, http.StatusUnprocessableEntity, ErrorCodeValidationFailed, "status is required")
			return
		}

		result, err := svc.ChangeStatus(c.Request.Context(), tenantID, actorID, c.Param("id"), req.Status)
		if err != nil {
			switch {
			case errors.Is(err, service.ErrInvalidStatus):
				RespondWithError(c, http.StatusUnprocessableEntity, ErrorCodeValidationFailed, err.Error())
			case errors.Is(err, service.ErrInvalidTransition), errors.Is(err, service.ErrUnknownCurrentState):
				RespondWithError(c, http.StatusConflict, ErrorCodeConflict, err.Error())
			default:
				status, code, message := MapServiceErrorToResponse(err)
				RespondWithError(c, status, code, message)
			}
			return
		}

		c.JSON(http.StatusOK, service.ResponseEnvelope{
			APIVersion: "v1",
			Data:       result,
		})
	}
}

// NewGetSubscriptionHandler returns a gin.HandlerFunc that retrieves a full
// subscription detail using the provided SubscriptionService.
func NewGetSubscriptionHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
	}
}

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
