package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/outbox"
)

// WebhookHandler handles incoming webhook events and persists them to the outbox.
type WebhookHandler struct {
	store outbox.Repository
	// replayGuard rejects duplicate event IDs in-memory (defense in depth).
	// The outbox repository remains the durable authority via a UNIQUE index
	// on deduplication_id.
	replayGuard *middleware.EventIDCache
}

// NewWebhookHandler creates a new webhook handler with the given outbox repository.
// The replay cache uses a five-minute TTL matching the webhook timestamp tolerance.
func NewWebhookHandler(store outbox.Repository) *WebhookHandler {
	return &WebhookHandler{
		store:       store,
		replayGuard: middleware.NewEventIDCache(5 * time.Minute),
	}
}

// Receive handles incoming webhook events.
// It expects the webhook verification middleware to have already verified the signature
// and set the following context values:
// - webhook_event_id: unique event ID from the provider header (generic)
// - webhook_provider: provider name (stripe, generic, etc.)
// - webhook_raw_body: raw request body for deduplication
//
// Providers that do not send an event ID header (e.g. Stripe, which signs the
// timestamp+payload) have the event ID extracted from the JSON payload instead.
func (h *WebhookHandler) Receive(c *gin.Context) {
	// The outbox store is nil in development mode (no database configured).
	// Acknowledge signature-valid delivery cannot be persisted, so fail loudly.
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "outbox_unavailable",
			"message": "webhook persistence is not configured",
		})
		return
	}

	provider, _ := c.Get("webhook_provider")
	providerStr, _ := provider.(string)
	rawBody, _ := c.Get("webhook_raw_body")
	bodyBytes, _ := rawBody.([]byte)

	// Parse the webhook payload to extract event type, event ID, and data.
	var payload map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_payload",
			"message": "invalid JSON payload",
		})
		return
	}

	// Event ID resolution: prefer the provider header set by the verification
	// middleware, then fall back to the payload's own ID field so providers
	// like Stripe (which transmit the ID in the body) are also protected.
	eventIDStr := c.GetString("webhook_event_id")
	if eventIDStr == "" {
		if v, ok := payload["id"].(string); ok && v != "" {
			eventIDStr = v
		} else if v, ok := payload["event_id"].(string); ok && v != "" {
			eventIDStr = v
		}
	}

	if eventIDStr == "" || providerStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "missing_tracking_identifiers",
			"message": "event ID and provider are required",
		})
		return
	}

	// Reject replays keyed by event_id before attempting persistence.
	if h.replayGuard.Has(c.Request.Context(), eventIDStr) {
		c.JSON(http.StatusOK, gin.H{
			"status":       "accepted",
			"event_id":     eventIDStr,
			"deduplicated": true,
			"message":      "duplicate event id",
		})
		return
	}

	eventType, _ := payload["type"].(string)
	if eventType == "" {
		eventType, _ = payload["event_type"].(string)
	}

	// Create outbox event for async processing.
	event, err := outbox.NewEventWithDeduplication(
		eventType,
		payload,
		nil, // aggregateID - can be extracted from payload if needed
		nil, // aggregateType
		&eventIDStr,
	)
	if err != nil {
		// Log error but don't expose internal details
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed_to_create_event",
		})
		return
	}

	// Persist to outbox. A duplicate event ID is rejected by the UNIQUE index
	// on deduplication_id; recast that as a successful idempotent ack so the
	// provider stops retrying a delivery that is already recorded.
	if err := h.store.Store(c.Request.Context(), event); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "accepted",
			"message": "event already processed",
		})
		return
	}

	// Record the event id in the in-memory replay cache now that it is durably
	// stored, so a fast retry within the TTL is acknowledged without re-persisting.
	_ = h.replayGuard.CheckAndStore(c.Request.Context(), eventIDStr)

	// Return 202 Accepted for successful persistence
	c.JSON(http.StatusAccepted, gin.H{
		"status":     "accepted",
		"event_id":   event.ID.String(),
		"event_type": eventType,
	})
}

// WebhookEvent represents a webhook event payload for testing
type WebhookEvent struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data"`
	CreatedAt int64                  `json:"created_at"`
}
