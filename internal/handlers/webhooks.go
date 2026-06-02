package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
