package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/outbox"
)

// NewWebhookHandler creates a handler that persists verified webhook events to outbox
func NewWebhookHandler(outboxRepo outbox.Repository) gin.HandlerFunc {
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

		// Create outbox event data
		subscriberID := c.GetHeader("X-Subscriber-ID")
		eventData := struct {
			Provider      string          `json:"provider"`
			SubscriberID  string          `json:"subscriber_id"`
			RawPayload    json.RawMessage `json:"raw_payload"`
		}{
			Provider:     providerStr,
			SubscriberID: subscriberID,
			RawPayload:   bodyBytes,
		}

		aggregateType := "subscriber"
		var aggregateID *string
		if subscriberID != "" {
			aggregateID = &subscriberID
		}

		// Create and store outbox event
		outboxEvent, err := outbox.NewEventWithDeduplication(
			"webhook.received",
			eventData,
			aggregateID,
			&aggregateType,
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
