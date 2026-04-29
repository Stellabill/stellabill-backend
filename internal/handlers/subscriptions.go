package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
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

func (h *Handler) ListSubscriptions(c *gin.Context) {
	// Delegate to the injected service/repo. Keep behavior minimal and compatible with tests.
	subs, err := h.Subscriptions.ListSubscriptions(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"subscriptions": subs})
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
		callerID, exists := c.Get("callerID")
		if !exists {
			RespondWithAuthError(c, "unauthorized")
			return
		}

		tenantID, exists := c.Get("tenantID")
		if !exists {
			RespondWithAuthError(c, "tenant id required")
			return
		}

		if _, err := requestparams.SanitizeQuery(c.Request.URL.Query(), requestparams.QueryRules{}); err != nil {
			RespondWithValidationError(c, err.Error(), nil)
			return
		}

		id, err := requestparams.NormalizePathID("id", c.Param("id"))
		if err != nil {
			RespondWithValidationError(c, err.Error(), map[string]interface{}{"field": "id"})
			return
		}

		detail, warnings, err := svc.GetDetail(c.Request.Context(), tenantID.(string), callerID.(string), id)
		if err != nil {
			status, code, message := MapServiceErrorToResponse(err)
			RespondWithError(c, status, code, message)
			return
		}

		c.Header("Content-Type", "application/json; charset=utf-8")
		c.JSON(http.StatusOK, service.ResponseEnvelope{
			APIVersion: "1",
			Data:       detail,
			Warnings:   warnings,
		})
	}
}

// UpdateSubscriptionStatus handles status updates with validation
func UpdateSubscriptionStatus(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subscription id required"})
		return
	}

	var payload struct {
		Status string `json:"status" binding:"required"`
	}

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// TODO: fetch current subscription from DB
	currentStatus := "active" // placeholder

	if err := subscriptions.CanTransition(currentStatus, payload.Status); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": err.Error(),
		})
		return
	}

	// TODO: persist update

	c.JSON(http.StatusOK, gin.H{
		"id":     id,
		"status": payload.Status,
	})
}