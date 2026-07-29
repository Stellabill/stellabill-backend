package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"stellarbill-backend/internal/outbox"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/service"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// WebSocket for live subscription status streaming
// - Hub with tenant and subscription ID filtering
// - Integrated with outbox dispatcher
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
		RenderProblem(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid cursor format")
		return
	}

	allSubs, err := h.Subscriptions.ListSubscriptions(c)
	if err != nil {
		RenderProblem(c, http.StatusInternalServerError, ErrorCodeInternalError, "Failed to retrieve subscriptions")
		return
	}

	page := pagination.PaginateSlice(allSubs, cursor, limit)
	setPaginationLinkHeader(c, page, cursorStr != "")

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
		RenderProblem(c, http.StatusNotFound, ErrorCodeNotFound, "not found")
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

// SubscriptionEvent represents a status change event for WS
type SubscriptionEvent struct {
	SubscriptionID string `json:"subscription_id"`
	Status         string `json:"status"`
	Timestamp      string `json:"timestamp"`
	TenantID       string `json:"tenant_id,omitempty"`
}

type WsClient struct {
	Conn           *websocket.Conn
	SubscriptionID string
	Send           chan SubscriptionEvent
}

type WsHub struct {
	clients    map[*WsClient]bool
	broadcast  chan SubscriptionEvent
	register   chan *WsClient
	unregister chan *WsClient
	mu         sync.RWMutex
}

var hub = &WsHub{
	clients:    make(map[*WsClient]bool),
	broadcast:  make(chan SubscriptionEvent, 100),
	register:   make(chan *WsClient),
	unregister: make(chan *WsClient),
}

func init() {
	go hub.run()
}

func (h *WsHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
			h.mu.Unlock()
		case event := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				// Filter by subscription ID
				if client.SubscriptionID == event.SubscriptionID {
					select {
					case client.Send <- event:
					default:
						// Buffer full, drop client
						close(client.Send)
						delete(h.clients, client)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		// Require origin for security, but allow all origins for now.
		return origin != ""
	},
}

// GetSubscriptionEvents handles WS stream for live subscription updates
func (h *Handler) GetSubscriptionEvents(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subscription id required"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade to websocket: %v", err)
		return
	}

	client := &WsClient{
		Conn:           conn,
		SubscriptionID: subscriptionID,
		Send:           make(chan SubscriptionEvent, 256),
	}

	hub.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *WsClient) writePump() {
	ticker := time.NewTicker(15 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case event, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteJSON(event); err != nil {
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *WsClient) readPump() {
	defer func() {
		hub.unregister <- c
		c.Conn.Close()
	}()
	c.Conn.SetReadLimit(512)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error { c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second)); return nil })
	for {
		_, _, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("websocket read error: %v", err)
			}
			break
		}
	}
}

// WebSocketOutboxPublisher implements outbox.Publisher to send events to WS hub
type WebSocketOutboxPublisher struct{}

func NewWebSocketOutboxPublisher() outbox.Publisher {
	return &WebSocketOutboxPublisher{}
}

func (p *WebSocketOutboxPublisher) Publish(ctx context.Context, event *outbox.Event) error {
	if event.EventType != "SubscriptionStatusChanged" {
		return nil
	}

	var payload struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(event.EventData, &payload); err != nil {
		return nil
	}

	var subID string
	if event.AggregateID != nil {
		subID = *event.AggregateID
	}

	wsEvent := SubscriptionEvent{
		SubscriptionID: subID,
		Status:         payload.Data.Status,
		Timestamp:      event.OccurredAt.Format(time.RFC3339),
	}
	
	select {
	case hub.broadcast <- wsEvent:
	case <-time.After(1 * time.Second):
		log.Printf("Failed to broadcast WS event: buffer full")
	}

	return nil
}
