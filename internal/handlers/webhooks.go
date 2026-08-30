package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/outbox"
)

// InboxRepository persists verified webhook events to an asynchronous inbox.
// It guarantees a 202 Accepted response to isolate receiver latency from
// internal processing bottlenecks.
type InboxRepository interface {
	Insert(ctx context.Context, provider, msgID, sourceID string, payload []byte) error
}

// NewVerifiedWebhookHandler creates a handler that persists verified webhook events
// to an asynchronous inbox.
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

// WebhookEvent represents an inbound webhook payload.
type WebhookEvent struct {
	EventType string                 `json:"event_type" binding:"required"`
	Data      map[string]interface{} `json:"data"       binding:"required"`
}

// WebhookHandler handles inbound webhook events from external systems.
//
// When constructed with an outbox repository, accepted events are persisted to
// the outbox (keyed by the deduplication ID extracted from the verified event
// ID) so they can be processed asynchronously. Replays are rejected upstream
// by the webhook verification middleware.
type WebhookHandler struct {
	store outbox.Repository
}

// NewWebhookHandler constructs a WebhookHandler. Provide an optional outbox
// repository to persist accepted events for async processing.
func NewWebhookHandler(stores ...outbox.Repository) *WebhookHandler {
	var store outbox.Repository
	if len(stores) > 0 && stores[0] != nil {
		store = stores[0]
	}
	return &WebhookHandler{store: store}
}

// Receive accepts an inbound webhook event, validates its structure, persists
// it to the outbox when one is configured, and dispatches it to the
// appropriate internal processor.
//
// POST /webhooks
//
// Supported event types:
//   - subscription.created — a new subscription has been provisioned
//   - statement.issued     — a billing statement has been generated
//
// Unknown event types are rejected with 422 so consumers get a clear diff
// rather than a silent 200.
func (wh *WebhookHandler) Receive(c *gin.Context) {
	// Capture the raw body (as verified by the middleware) before binding so
	// accepted events can be persisted verbatim to the outbox.
	rawBody := getWebhookRawBody(c)

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
		subscriptionID, _ := event.Data["subscription_id"].(string)
		if subscriptionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "missing_field",
				"message": "data.subscription_id is required",
			})
			return
		}
		if err := wh.persist(c, event, rawBody); err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":   "failed_to_store_webhook_event",
				"message": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":          "accepted",
			"event_type":      event.EventType,
			"subscription_id": subscriptionID,
		})
	case "statement.issued":
		statementID, _ := event.Data["statement_id"].(string)
		if statementID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "missing_field",
				"message": "data.statement_id is required",
			})
			return
		}
		if err := wh.persist(c, event, rawBody); err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":   "failed_to_store_webhook_event",
				"message": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":       "accepted",
			"event_type":   event.EventType,
			"statement_id": statementID,
		})
	default:
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":      "unknown_event_type",
			"message":    "unrecognised event_type: " + event.EventType,
			"event_type": event.EventType,
		})
	}
}

// persist writes the verified, accepted webhook event to the outbox. The raw
// body captured by the verification middleware is stored so downstream
// processors can re-decode it, and the verified event ID is used as the
// deduplication key so duplicate deliveries do not create duplicate events.
func (wh *WebhookHandler) persist(c *gin.Context, event WebhookEvent, rawBody []byte) error {
	if wh.store == nil {
		return nil
	}

	provider, _ := c.Get("webhook_provider")
	providerStr, _ := provider.(string)

	eventID, _ := c.Get("webhook_event_id")
	eventIDStr, _ := eventID.(string)

	subscriberID := c.GetHeader("X-Subscriber-ID")
	eventData := struct {
		Provider     string          `json:"provider"`
		EventType    string          `json:"event_type"`
		SubscriberID string          `json:"subscriber_id"`
		RawPayload   json.RawMessage `json:"raw_payload"`
	}{
		Provider:     providerStr,
		EventType:    event.EventType,
		SubscriberID: subscriberID,
		RawPayload:   rawBody,
	}

	aggregateType := "subscriber"
	var aggregateID *string
	if subscriberID != "" {
		aggregateID = &subscriberID
	}

	var dedupID *string
	if eventIDStr != "" {
		dedupID = &eventIDStr
	}

	outboxEvent, err := outbox.NewEventWithDeduplication(
		"webhook.received",
		eventData,
		aggregateID,
		&aggregateType,
		dedupID,
	)
	if err != nil {
		return err
	}

	return wh.store.Store(c.Request.Context(), outboxEvent)
}

// getWebhookRawBody returns the raw request body captured by the webhook
// verification middleware from the gin context, falling back to reading the
// request body directly when the middleware did not populate it.
func getWebhookRawBody(c *gin.Context) []byte {
	if rb, ok := c.Get("webhook_raw_body"); ok {
		if body, ok2 := rb.([]byte); ok2 {
			return body
		}
	}
	if data, err := c.GetRawData(); err == nil {
		return data
	}
	return nil
}
