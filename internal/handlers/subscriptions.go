package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/db"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/subscriptions"
	"stellarbill-backend/internal/validation"
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
		// Minimal, safe handler that validates caller and path, then delegates to the service.
		callerID, exists := c.Get("callerID")
		if !exists {
			RespondWithAuthError(c, "unauthorized")
			return
		}

		// 0. Reject unexpected query params for strict alignment with OpenAPI
		if len(c.Request.URL.Query()) > 0 {
			RespondWithValidationFields(c, "Unexpected query parameters", nil)
			return
		}

		id := c.Param("id")
		id = strings.TrimSpace(id)
		id = validation.NormalizeString(id)
		if err := validation.ValidateUUID(id); err != nil {
			RespondWithValidationFields(c, "Invalid subscription ID", validation.ValidateVar(id, "required,uuid"))
			return
		}
		tenantID, _ := c.Get("tenantID")

		// Delegate to service (note: real implementation may include ownership checks)
		detail, warnings, err := svc.GetDetail(c.Request.Context(), tenantID.(string), callerID.(string), id)
		if err != nil {
			statusCode, errorCode, msg := MapServiceErrorToResponse(err)
			RespondWithError(c, statusCode, errorCode, msg)
			return
		}

		c.JSON(http.StatusOK, service.ResponseEnvelope{
			APIVersion: "1",
			Data:       detail,
			Warnings:   warnings,
		})
	}
}

// UpdateSubscriptionStatus handles status updates with validation and atomic outbox publishing
func (h *Handler) UpdateSubscriptionStatus(c *gin.Context) {
	id := c.Param("id")
	if err := validation.ValidateUUID(id); err != nil {
		RespondWithValidationFields(c, "Invalid subscription ID", []validation.FieldError{
			{Field: "id", Message: "ID must be a valid UUID"},
		})
		return
	}

	var payload struct {
		Status string `json:"status" validate:"required,oneof=active cancelled expired pending"`
	}

	if fieldErrors := validation.BindAndValidateJSON(c, &payload); len(fieldErrors) > 0 {
		RespondWithValidationFields(c, "Validation failed", fieldErrors)
		return
	}

	// Use RunInTransaction for atomicity
	err := db.RunInTransaction(c.Request.Context(), h.DB, func(tx *sql.Tx) error {
		// 1. Fetch current subscription to check transition
		// We use the repository with the transaction
		subRepo := h.SubRepo.WithTx(tx)
		sub, err := subRepo.GetByID(id)
		if err != nil {
			return err
		}

		// 2. Validate transition
		if err := subscriptions.CanTransition(sub.Status, payload.Status); err != nil {
			return err // Will be handled outside to return 422
		}

		// 3. Update status
		if err := subRepo.UpdateStatus(id, payload.Status); err != nil {
			return err
		}

		// 4. Publish outbox event
		eventData := map[string]interface{}{
			"subscription_id": id,
			"old_status":      sub.Status,
			"new_status":      payload.Status,
		}
		
		// Use a deterministic deduplication ID for idempotency if provided in headers (optional)
		// For now, we'll generate one based on ID and Status to prevent duplicate transitions to same status
		dedupID := fmt.Sprintf("sub_status_update_%s_%s", id, payload.Status)
		
		_, err = h.Outbox.PublishEventWithTx(tx, "subscription.status_updated", eventData, &id, nil, &dedupID)
		return err
	})

	if err != nil {
		// Handle specific errors
		if err.Error() == "subscription not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		
		// Check if it's a transition error (this is a bit hacky since we lose type info in RunInTransaction)
		// In a real app, we'd use custom error types
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":     id,
		"status": payload.Status,
	})
}