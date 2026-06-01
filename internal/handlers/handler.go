package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"stellarbill-backend/internal/audit"
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
	OutboxRepo    outbox.Repository
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

// ListDeadLetteredEvents handles GET /api/admin/outbox/dead-letter
func (h *Handler) ListDeadLetteredEvents(c *gin.Context) {
	if h.OutboxRepo == nil {
		RespondWithError(c, http.StatusServiceUnavailable, ErrorCodeInternal, "outbox repository not available")
		return
	}

	limit := 100
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	events, err := h.OutboxRepo.ListDeadLetteredEvents(limit)
	if err != nil {
		RespondWithError(c, http.StatusInternalServerError, ErrorCodeInternal, "failed to list dead-lettered events")
		return
	}

	c.JSON(http.StatusOK, events)
}

// RequeueOutboxEvent handles POST /api/admin/outbox/:id/requeue
func (h *Handler) RequeueOutboxEvent(c *gin.Context) {
	if h.OutboxRepo == nil {
		RespondWithError(c, http.StatusServiceUnavailable, ErrorCodeInternal, "outbox repository not available")
		return
	}

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		RespondWithError(c, http.StatusBadRequest, ErrorCodeInvalidRequest, "invalid event ID")
		return
	}

	err = h.OutboxRepo.RequeueEvent(id)
	if err != nil {
		if err.Error() == "event not found or not in failed status" {
			RespondWithError(c, http.StatusNotFound, ErrorCodeNotFound, err.Error())
			return
		}
		RespondWithError(c, http.StatusInternalServerError, ErrorCodeInternal, "failed to requeue event")
		return
	}

	audit.LogAction(c, "outbox_requeue", idStr, "success", nil)

	c.Status(http.StatusNoContent)
}
