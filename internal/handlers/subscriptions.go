package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"stellabill-backend/internal/requestparams"
	"stellabill-backend/internal/service"
	"stellabill-backend/internal/subscriptions"
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
	c.JSON(http.StatusOK, gin.H{"subscriptions": []Subscription{}})
}

func GetSubscription(c *gin.Context) {
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
		// Minimal, safe handler that validates caller and path, then delegates to the service.
		callerID, exists := c.Get("callerID")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		if _, err := requestparams.SanitizeQuery(c.Request.URL.Query(), requestparams.QueryRules{}); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		id, err := requestparams.NormalizePathID("id", c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Delegate to service (note: real implementation may include ownership checks)
		tenantID, _ := c.Get("tenantID")
		role, _ := c.Get("role")
		isAdmin := role == "admin"

		_, _, err = svc.GetDetail(c.Request.Context(), tenantID.(string), callerID.(string), id, isAdmin)
		if err != nil {
			switch err {
			case service.ErrNotFound:
				c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
			case service.ErrForbidden:
				c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			case service.ErrDeleted:
				c.JSON(http.StatusGone, gin.H{"error": "subscription has been deleted"})
			case service.ErrBillingParse:
				c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "billing data error"})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{"id": id})
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

