package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

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

func ListSubscriptions(c *gin.Context) {
	// TODO: load from DB, filter by merchant from JWT/API key
	subscriptions := []Subscription{}
	c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
}

// NewGetSubscriptionHandler returns a gin.HandlerFunc that retrieves a full
// subscription detail using the provided SubscriptionService.
func NewGetSubscriptionHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Read callerID from context (set by AuthMiddleware).
		callerID, exists := c.Get("callerID")
		if !exists {
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// 2. Validate :id path param.
		id := c.Param("id")
		if strings.TrimSpace(id) == "" {
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.JSON(http.StatusBadRequest, gin.H{"error": "subscription id required"})
			return
		}

		// 3. Call service.
		detail, warnings, err := svc.GetDetail(c.Request.Context(), callerID.(string), id)
		if err != nil {
			c.Header("Content-Type", "application/json; charset=utf-8")
			switch err {
			case service.ErrNotFound:
				c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
			case service.ErrDeleted:
				c.JSON(http.StatusGone, gin.H{"error": "subscription has been deleted"})
			case service.ErrForbidden:
				c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			case service.ErrBillingParse:
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			}
			return
		}

		// 4. Set Content-Type and respond with envelope.
		c.Header("Content-Type", "application/json; charset=utf-8")
		envelope := service.ResponseEnvelope{
			APIVersion: "1",
			Data:       detail,
			Warnings:   warnings,
		}
		c.JSON(http.StatusOK, envelope)
	}
}
