package webhook

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler processes webhook events with idempotency
type Handler struct {
	store *Store
}

// NewHandler creates a new webhook handler
func NewHandler(store *Store) *Handler {
	return &Handler{
		store: store,
	}
}

// WebhookRequest represents an incoming webhook request
type WebhookRequest struct {
	ProviderEventID string `json:"provider_event_id" binding:"required"`
	TenantID        string `json:"tenant_id" binding:"required"`
	EventType       string `json:"event_type" binding:"required"`
	Data            interface{} `json:"data"`
}

// HandleWebhook processes a webhook event with idempotency
// Returns 200 OK for duplicates (without reprocessing) or 202 Accepted for new events
func (h *Handler) HandleWebhook(c *gin.Context) {
	var req WebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request",
			"message": err.Error(),
		})
		return
	}

	// Create event record
	event := &Event{
		ProviderEventID: req.ProviderEventID,
		TenantID:        req.TenantID,
		EventType:       req.EventType,
	}

	// Check for duplicate
	isDuplicate := h.store.CheckAndStore(event)

	if isDuplicate {
		// Duplicate event - return 200 OK without reprocessing
		log.Printf("[WEBHOOK] Duplicate event received: provider_event_id=%s tenant_id=%s event_type=%s", 
			req.ProviderEventID, req.TenantID, req.EventType)
		c.JSON(http.StatusOK, gin.H{
			"status": "duplicate",
			"message": "Event already processed",
			"provider_event_id": req.ProviderEventID,
		})
		return
	}

	// New event - process it
	log.Printf("[WEBHOOK] Processing new event: provider_event_id=%s tenant_id=%s event_type=%s", 
		req.ProviderEventID, req.TenantID, req.EventType)

	// TODO: Add actual webhook processing logic here
	// This would typically involve:
	// - Validating the webhook signature
	// - Processing the event data
	// - Updating business logic

	c.JSON(http.StatusAccepted, gin.H{
		"status": "accepted",
		"message": "Event accepted for processing",
		"provider_event_id": req.ProviderEventID,
	})
}
