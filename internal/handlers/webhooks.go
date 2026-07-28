package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
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