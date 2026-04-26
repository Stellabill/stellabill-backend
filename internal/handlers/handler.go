package handlers

import (
	"database/sql"
	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/outbox"
	"stellarbill-backend/internal/repositories"
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
	DB            *sql.DB
	OutboxSvc     *outbox.Service
	Database      interface{} // DBPinger - dependency for health checks
	Outbox        interface{} // OutboxHealther - dependency for queue health checks
	SubRepo       repositories.SubscriptionRepository
	PlanRepo      repositories.PlanRepository
}

// NewHandler creates a new Handler with the given dependencies
func NewHandler(plans PlanService, subscriptions SubscriptionService, db *sql.DB, outboxSvc *outbox.Service) *Handler {
	return &Handler{
		Plans:         plans,
		Subscriptions: subscriptions,
		DB:            db,
		Outbox:        outboxSvc,
	}
}

// NewHandlerWithDependencies creates a new Handler with all dependencies
func NewHandlerWithDependencies(
	plans PlanService,
	subscriptions SubscriptionService,
	db interface{},
	outbox interface{},
) *Handler {
	return &Handler{
		Plans:         plans,
		Subscriptions: subscriptions,
		Database:      db,
		Outbox:        outbox,
	}
}
