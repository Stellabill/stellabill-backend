package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Subscription struct {
	ID        string `json:"id"`
	PlanID    string `json:"plan_id"`
	Customer  string `json:"customer"`
	Status    string `json:"status"`
	Amount    string `json:"amount"`
	Interval  string `json:"interval"`
	NextBilling string `json:"next_billing,omitempty"`
}

func (h *Handler) ListSubscriptions(c *gin.Context) {
	subscriptions, err := h.Subscriptions.ListSubscriptions(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
}

func (h *Handler) GetSubscription(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subscription id required"})
		return
	}

	sub, err := h.Subscriptions.GetSubscription(c, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if sub == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
		return
	}

	c.JSON(http.StatusOK, sub)
}
