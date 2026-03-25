package handlers

import (
	"github.com/gin-gonic/gin"
)

// PlanService defines the interface for plan-related operations
type PlanService interface {
	ListPlans(c *gin.Context) ([]Plan, error)
}

// SubscriptionService defines the interface for subscription-related operations
type SubscriptionService interface {
	ListSubscriptions(c *gin.Context) ([]Subscription, error)
	GetSubscription(c *gin.Context, id string) (*Subscription, error)
}

// Handler holds the dependencies for the HTTP handlers
type Handler struct {
	Plans         PlanService
	Subscriptions SubscriptionService
}

// NewHandler creates a new Handler with the given dependencies
func NewHandler(plans PlanService, subscriptions SubscriptionService) *Handler {
	return &Handler{
		Plans:         plans,
		Subscriptions: subscriptions,
	}
}
