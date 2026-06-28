package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// WebhookEvent represents an inbound webhook payload.
type WebhookEvent struct {
	EventType string                 `json:"event_type" binding:"required"`
	Data      map[string]interface{} `json:"data"       binding:"required"`
}

// WebhookHandler handles inbound webhook events from external systems.
type WebhookHandler struct{}

// NewWebhookHandler constructs a WebhookHandler.
func NewWebhookHandler() *WebhookHandler {
	return &WebhookHandler{}
}

// Receive accepts an inbound webhook event, validates its structure, and
// dispatches it to the appropriate internal processor.
//
// POST /webhooks
//
// Supported event types:
//   - subscription.created  — a new subscription has been provisioned
//   - statement.issued      — a billing statement has been generated
//
// Unknown event types are rejected with 422 so consumers get a clear
// diff rather than a silent 200.
func (wh *WebhookHandler) Receive(c *gin.Context) {
	var event WebhookEvent
	if err := c.ShouldBindJSON(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_payload",
			"message": err.Error(),
		})
		return
	}

	switch event.EventType {
	case "subscription.created":
		wh.handleSubscriptionCreated(c, event)
	case "statement.issued":
		wh.handleStatementIssued(c, event)
	default:
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":      "unknown_event_type",
			"message":    "unrecognised event_type: " + event.EventType,
			"event_type": event.EventType,
		})
	}
}

func (wh *WebhookHandler) handleSubscriptionCreated(c *gin.Context, event WebhookEvent) {
	subscriptionID, _ := event.Data["subscription_id"].(string)
	if subscriptionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "missing_field",
			"message": "data.subscription_id is required",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":          "accepted",
		"event_type":      event.EventType,
		"subscription_id": subscriptionID,
	})
}

func (wh *WebhookHandler) handleStatementIssued(c *gin.Context, event WebhookEvent) {
	statementID, _ := event.Data["statement_id"].(string)
	if statementID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "missing_field",
			"message": "data.statement_id is required",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "accepted",
		"event_type":   event.EventType,
		"statement_id": statementID,
	})
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
