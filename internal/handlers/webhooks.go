package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// WebhookEvent is the generic JSON envelope body received at POST /webhooks.
type WebhookEvent struct {
	EventType string                 `json:"event_type"`
	Data      map[string]interface{} `json:"data"`
}

// WebhookHandler dispatches validated inbound webhook events.
type WebhookHandler struct{}

// NewWebhookHandler constructs a WebhookHandler.
func NewWebhookHandler() *WebhookHandler {
	return &WebhookHandler{}
}

// Receive accepts an inbound webhook event, validates its structure, and
// returns an accepted response for supported event types.
//
// POST /webhooks
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
}

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
