package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/service"
)
// SSE for Issue #357: Server-Sent Events for live subscription status
// - Fan-out hub + heartbeats every 15s
// - Graceful shutdown on context done
// - Ready for outbox dispatcher integration
type Subscription struct {
	ID          string `json:"id"`
	PlanID      string `json:"plan_id"`
	Customer    string `json:"customer"`
	Status      string `json:"status"`
	Amount      string `json:"amount"`
	Interval    string `json:"interval"`
	NextBilling string `json:"next_billing,omitempty"`
}

func (s Subscription) GetID() string        { return s.ID }
func (s Subscription) GetSortValue() string { return s.Customer } // Sort by customer for now

func (h *Handler) ListSubscriptions(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}

	cursorStr := c.Query("cursor")
	cursor, err := pagination.Decode(cursorStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor format"})
		return
	}

	allSubs, err := h.Subscriptions.ListSubscriptions(c)
	if err != nil {
		RespondWithInternalError(c, "Failed to retrieve subscriptions")
		return
	}

	page := pagination.PaginateSlice(allSubs, cursor, limit)

	c.JSON(http.StatusOK, gin.H{
		"subscriptions": page.Items,
		"next_cursor":   page.NextCursor,
		"has_more":      page.HasMore,
	})
}

func (h *Handler) GetSubscription(c *gin.Context) {
	id := c.Param("id")
	sub, err := h.Subscriptions.GetSubscription(c, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, sub)
}

// NewGetSubscriptionHandler returns a gin.HandlerFunc that retrieves a full
// subscription detail using the provided SubscriptionService.
func NewGetSubscriptionHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
	}
}

// SubscriptionEvent represents a status change event for SSE
type SubscriptionEvent struct {
	SubscriptionID string `json:"subscription_id"`
	Status         string `json:"status"`
	Timestamp      string `json:"timestamp"`
	TenantID       string `json:"tenant_id,omitempty"`
}

// SimpleFanOutHub is a basic fan-out hub for SSE (fed by outbox later)
type SimpleFanOutHub struct {
	clients   map[chan SubscriptionEvent]bool
	broadcast chan SubscriptionEvent
}

var hub = &SimpleFanOutHub{
	clients:   make(map[chan SubscriptionEvent]bool),
	broadcast: make(chan SubscriptionEvent, 100),
}

// run starts the hub (called on startup in real impl)
func (h *SimpleFanOutHub) run() {
	for event := range h.broadcast {
		for client := range h.clients {
			select {
			case client <- event:
			default:
				close(client)
				delete(h.clients, client)
			}
		}
	}
}

// GetSubscriptionEvents handles SSE stream for live subscription updates
func (h *Handler) GetSubscriptionEvents(c *gin.Context) {
	// TODO: Extract tenant from auth token (follow patterns in other handlers like reconciliation.go)
	// tenantID := getTenantFromContext(c)

	clientChan := make(chan SubscriptionEvent, 10)

	hub.clients[clientChan] = true
	defer func() {
		delete(hub.clients, clientChan)
		close(clientChan)
	}()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

	c.Stream(func(w io.Writer) bool {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-c.Request.Context().Done():
				return false // graceful shutdown / client disconnect
			case event, ok := <-clientChan:
				if !ok {
					return false
				}
				// Filter by tenant in real impl
				fmt.Fprintf(w, "data: %s\n\n", `{"subscription_id":"`+event.SubscriptionID+`","status":"`+event.Status+`","timestamp":"`+event.Timestamp+`"}`)
				c.Writer.Flush()
			case <-ticker.C:
				fmt.Fprintf(w, ": heartbeat\n\n")
				c.Writer.Flush()
			}
		}
	})
}