package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/outbox"
)

type InboxRepository interface {
	Insert(ctx context.Context, provider, msgID, sourceID string, payload []byte) error
}

// NewVerifiedWebhookHandler creates a handler that persists verified webhook events 
// to an asynchronous inbox. It guarantees a 202 Accepted response to isolate 
// receiver latency from internal processing bottlenecks.
func NewVerifiedWebhookHandler(inboxRepo InboxRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		eventID, _ := c.Get("webhook_event_id")
		provider, _ := c.Get("webhook_provider")
		rawBody, _ := c.Get("webhook_raw_body")

		eventIDStr, _ := eventID.(string)
		providerStr, _ := provider.(string)
		bodyBytes, _ := rawBody.([]byte)

		subscriberID := c.GetHeader("X-Subscriber-ID")

		if eventIDStr == "" || providerStr == "" || subscriberID == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error":   "missing_tracking_identifiers",
				"message": "event ID, provider, and subscriber ID are required",
			})
			return
		}

		err := inboxRepo.Insert(c.Request.Context(), providerStr, eventIDStr, subscriberID, bodyBytes)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "failed_to_persist_webhook",
			})
			return
		}

		c.JSON(http.StatusAccepted, gin.H{
		"status": "accepted",
		})
	}
}

// OutboxEventStorer is the minimal outbox surface required by NewWebhookHandler.
type OutboxEventStorer interface {
	Store(event *outbox.Event) error
}

// NewWebhookHandler creates a handler that persists verified webhook events to the
// outbox under a deduplication key so retries collapse to a single event.
func NewWebhookHandler(outboxRepo OutboxEventStorer) gin.HandlerFunc {
	return func(c *gin.Context) {
		eventID, _ := c.Get("webhook_event_id")
		provider, _ := c.Get("webhook_provider")
		rawBody, _ := c.Get("webhook_raw_body")

		var eventIDStr string
		if eid, ok := eventID.(string); ok {
			eventIDStr = eid
		}

		var providerStr string
		if p, ok := provider.(string); ok {
			providerStr = p
		}

		var bodyBytes []byte
		if b, ok := rawBody.([]byte); ok {
			bodyBytes = b
		}

		// Reject malformed bodies before they are captured: json.RawMessage
		// bypasses validation during Marshal, so an invalid body would otherwise
		// be silently persisted into the outbox as corrupt event data.
		if !json.Valid(bodyBytes) {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to create outbox event"})
			return
		}

		// Create outbox event data
		eventData := struct {
			Provider   string          `json:"provider"`
			RawPayload json.RawMessage `json:"raw_payload"`
		}{
			Provider:   providerStr,
			RawPayload: bodyBytes,
		}

		// Create and store outbox event
		outboxEvent, err := outbox.NewEventWithDeduplication(
			"webhook.received",
			eventData,
			nil,
			nil,
			&eventIDStr,
		)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to create outbox event"})
			return
		}

		if err := outboxRepo.Store(outboxEvent); err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to store outbox event"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}