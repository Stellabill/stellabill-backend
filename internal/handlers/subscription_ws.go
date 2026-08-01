package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"stellarbill-backend/internal/outbox"
)

// WebSocket tuning values for subscription event streams.
const (
	// wsMaxMessageSize caps inbound frames; clients are read-only.
	wsMaxMessageSize = 512
	// wsSendBufferSize is the per-client outbound queue. A slow client whose
	// buffer fills is dropped rather than allowed to block the hub.
	wsSendBufferSize = 256
	// wsBroadcastQueueSize is the hub's inbound broadcast queue.
	wsBroadcastQueueSize = 100
	// subscriptionStatusChangedEvent is the outbox event type that carries
	// subscription lifecycle transitions to the hub.
	subscriptionStatusChangedEvent = "SubscriptionStatusChanged"
)

// Timing values are variables rather than constants so tests can shorten
// them. Production defaults implement the required 30s heartbeat with a 60s
// idle tolerance and a 10s write deadline.
var (
	// wsWriteWait is the max time allowed to write a frame to the peer.
	wsWriteWait = 10 * time.Second
	// wsPongWait is how long the server waits for a pong after sending a
	// ping. Exceeding it (client idle) triggers a clean close.
	wsPongWait = 60 * time.Second
	// wsPingPeriod is the heartbeat interval. It must be shorter than
	// wsPongWait so the read deadline is refreshed while the client lives.
	wsPingPeriod = 30 * time.Second
	// wsBroadcastTimeout bounds how long Broadcast waits to enqueue an event
	// before dropping it, keeping the outbox dispatcher unblocked.
	wsBroadcastTimeout = time.Second
)

// SubscriptionEvent is the payload streamed to connected clients when a
// subscription lifecycle transition occurs.
type SubscriptionEvent struct {
	SubscriptionID string `json:"subscription_id"`
	Status         string `json:"status"`
	Timestamp      string `json:"timestamp"`
	TenantID       string `json:"tenant_id,omitempty"`
}

// wsClient is a single connected WebSocket client. It is scoped to a
// (tenantID, subscriptionID) pair so a tenant can never observe another
// tenant's events.
type wsClient struct {
	conn           *websocket.Conn
	tenantID       string
	subscriptionID string
	send           chan SubscriptionEvent
	hub            *WsHub
}

// WsHub fans out subscription lifecycle events to connected clients.
//
// Security: every client is registered with the authenticated tenant ID and
// the subscribed subscription ID; broadcast events are only delivered when
// both match. This prevents cross-tenant event leakage.
//
// Backpressure: when a client's send buffer is full, the client is dropped
// and closed immediately instead of blocking the hub or the outbox
// dispatcher. The hub's own broadcast queue is bounded, and Broadcast never
// blocks the caller.
type WsHub struct {
	mu         sync.RWMutex
	clients    map[*wsClient]bool
	broadcast  chan SubscriptionEvent
	register   chan *wsClient
	unregister chan *wsClient
	done       chan struct{}
	closeOnce  sync.Once
}

// NewWsHub creates and starts a subscription event hub.
func NewWsHub() *WsHub {
	h := &WsHub{
		clients:    make(map[*wsClient]bool),
		broadcast:  make(chan SubscriptionEvent, wsBroadcastQueueSize),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
		done:       make(chan struct{}),
	}
	go h.run()
	return h
}

// Stop halts the hub's run loop. Idempotent and safe to call once per hub.
func (h *WsHub) Stop() {
	h.closeOnce.Do(func() { close(h.done) })
}

// run is the hub's single event loop. All client-map mutations happen here
// (or under the mutex inside dispatch), so there are no concurrent map
// accesses.
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
				close(client.send)
			}
			h.mu.Unlock()
		case event := <-h.broadcast:
			h.dispatch(event)
		case <-h.done:
			return
		}
	}
}

// dispatch delivers an event to every client matching tenant and
// subscription, dropping slow consumers.
func (h *WsHub) dispatch(event SubscriptionEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		if client.tenantID != event.TenantID || client.subscriptionID != event.SubscriptionID {
			continue
		}
		select {
		case client.send <- event:
		default:
			// Slow consumer: drop instead of blocking the hub.
			delete(h.clients, client)
			close(client.send)
		}
	}
}

// Broadcast enqueues an event for delivery to matching clients. It never
// blocks the caller; if the bounded broadcast queue is full the event is
// dropped and logged.
func (h *WsHub) Broadcast(event SubscriptionEvent) {
	select {
	case h.broadcast <- event:
	case <-time.After(wsBroadcastTimeout):
		log.Printf("subscription hub: broadcast buffer full, dropping event for subscription %s", event.SubscriptionID)
	}
}

// ClientCount returns the number of connected clients (used by tests and
// observability).
func (h *WsHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// wsOriginPolicy is the WebSocket origin allow-list, mirroring the CORS
// middleware semantics so dashboards configured for CORS also work for WS.
type wsOriginPolicy struct {
	mu      sync.RWMutex
	allowed map[string]bool
}

// set replaces the allow-list from a comma-separated string.
func (p *wsOriginPolicy) set(raw string) {
	allowed := make(map[string]bool)
	for _, o := range strings.Split(raw, ",") {
		o = strings.ToLower(strings.TrimSpace(o))
		if o != "" {
			allowed[o] = true
		}
	}
	p.mu.Lock()
	p.allowed = allowed
	p.mu.Unlock()
}

// allows reports whether an Origin header may open a WebSocket connection.
// A missing Origin (non-browser clients, curl, server-to-server) is allowed;
// otherwise the origin must be allow-listed, or the allow-list is empty or
// contains "*" (development default).
//
// Note: unlike the CORS middleware, which rejects "*" in production, the WS
// policy treats "*" as allow-all regardless of environment. WebSocket origins
// are not subject to preflight and the allow-list is enforced server-side, so
// this is acceptable; set ALLOWED_ORIGINS explicitly in production.
func (p *wsOriginPolicy) allows(origin string) bool {
	if origin == "" {
		return true
	}
	origin = strings.ToLower(origin)
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.allowed) == 0 || p.allowed["*"] {
		return true
	}
	return p.allowed[origin]
}

// wsOrigins is the process-wide origin policy for the default upgrader.
var wsOrigins = &wsOriginPolicy{}

// ConfigureWebSocketOrigins sets the WebSocket origin allow-list. It is
// called during route registration with the same ALLOWED_ORIGINS value used
// by the CORS middleware.
func ConfigureWebSocketOrigins(allowedOrigins string) {
	wsOrigins.set(allowedOrigins)
}

// newWSUpgrader builds a WebSocket upgrader that validates the Origin header
// against the configured allow-list before upgrading.
func newWSUpgrader() *websocket.Upgrader {
	return &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return wsOrigins.allows(r.Header.Get("Origin"))
		},
	}
}

// defaultHub is the process-wide hub used by the HTTP handler.
var defaultHub = NewWsHub()

// defaultUpgrader is the process-wide upgrader used by the HTTP handler.
var defaultUpgrader = newWSUpgrader()

// hub returns the handler's hub (or the package default when unset).
func (h *Handler) hub() *WsHub {
	if h.wsHub != nil {
		return h.wsHub
	}
	return defaultHub
}

// upgrader returns the handler's upgrader (or the package default when unset).
func (h *Handler) upgrader() *websocket.Upgrader {
	if h.wsUpgrader != nil {
		return h.wsUpgrader
	}
	return defaultUpgrader
}

// GetSubscriptionEvents upgrades the HTTP connection to a WebSocket and
// streams subscription lifecycle transitions for the authenticated tenant.
//
// The route is registered behind the JWT auth middleware; this handler
// additionally requires a valid tenant ID in the request context and a
// matching subscription ID path parameter. The Origin header is validated by
// the upgrader before the handshake completes.
func (h *Handler) GetSubscriptionEvents(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subscription id required"})
		return
	}

	tenantID, ok := c.Get("tenantID")
	if !ok {
		RespondWithAuthError(c, "Missing tenant context")
		return
	}
	tenantIDStr, ok := tenantID.(string)
	if !ok || tenantIDStr == "" {
		RespondWithAuthError(c, "Invalid tenant context")
		return
	}

	conn, err := h.upgrader().Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// The upgrader already wrote an HTTP error response (e.g. 403 for a
		// disallowed Origin, 400 for a non-WebSocket request).
		log.Printf("subscription events: upgrade failed for subscription %s: %v", subscriptionID, err)
		return
	}

	hub := h.hub()
	client := &wsClient{
		conn:           conn,
		tenantID:       tenantIDStr,
		subscriptionID: subscriptionID,
		send:           make(chan SubscriptionEvent, wsSendBufferSize),
		hub:            hub,
	}

	select {
	case hub.register <- client:
	case <-c.Request.Context().Done():
		_ = conn.Close()
		return
	}

	go client.writePump()
	go client.readPump()
}

// writePump writes events and heartbeat pings to the client. It exits on
// send-channel close (client dropped or unregistered), a write error, or an
// idle timeout, and always closes the connection.
func (c *wsClient) writePump() {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case event, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteJSON(event); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump drains inbound frames, refreshing the read deadline on pongs so
// the connection stays alive while the client responds to heartbeats. When
// the client goes idle past wsPongWait (or closes), the client is
// unregistered and the connection closed.
func (c *wsClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(wsMaxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("subscription events: read error: %v", err)
			}
			break
		}
	}
}

// WebSocketOutboxPublisher implements outbox.Publisher and bridges the outbox
// dispatcher to the hub. Every SubscriptionStatusChanged event is forwarded
// to the hub, which delivers it only to clients of the same tenant and
// subscription. Publishing never blocks: a full broadcast queue drops the
// event.
type WebSocketOutboxPublisher struct {
	hub *WsHub
}

// NewWebSocketOutboxPublisher creates a publisher wired to the given hub,
// falling back to the package default hub when hub is nil.
func NewWebSocketOutboxPublisher(hub *WsHub) outbox.Publisher {
	if hub == nil {
		hub = defaultHub
	}
	return &WebSocketOutboxPublisher{hub: hub}
}

// Publish implements outbox.Publisher.
func (p *WebSocketOutboxPublisher) Publish(_ context.Context, event *outbox.Event) error {
	if event == nil || event.EventType != subscriptionStatusChangedEvent {
		return nil
	}

	var envelope struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(event.EventData, &envelope); err != nil {
		return nil
	}
	// Skip events whose payload carries no status (e.g. JWE-encrypted events
	// whose EventData is opaque ciphertext, or differently-shaped payloads) so
	// clients never receive bogus empty-status frames.
	if envelope.Data.Status == "" {
		return nil
	}

	var subID string
	if event.AggregateID != nil {
		subID = *event.AggregateID
	}

	p.hub.Broadcast(SubscriptionEvent{
		SubscriptionID: subID,
		Status:         envelope.Data.Status,
		Timestamp:      event.OccurredAt.UTC().Format(time.RFC3339),
		TenantID:       event.TenantID,
	})
	return nil
}
