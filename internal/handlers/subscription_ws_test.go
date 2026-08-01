package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"stellarbill-backend/internal/outbox"
)

// ---------------------------------------------------------------------------
// Hub unit tests
// ---------------------------------------------------------------------------

func TestWsHub_RegisterBroadcastUnregister(t *testing.T) {
	h := NewWsHub()
	defer h.Stop()

	// Broadcast before any client exists is a no-op (no panic).
	h.Broadcast(SubscriptionEvent{SubscriptionID: "s1", TenantID: "t1"})
	assert.Equal(t, 0, h.ClientCount())

	client := &wsClient{
		tenantID:       "t1",
		subscriptionID: "s1",
		send:           make(chan SubscriptionEvent, 8),
		hub:            h,
	}
	h.register <- client
	assert.Equal(t, 1, h.ClientCount())

	// Matching tenant + subscription → delivered.
	h.Broadcast(SubscriptionEvent{SubscriptionID: "s1", TenantID: "t1", Status: "active"})
	select {
	case ev := <-client.send:
		assert.Equal(t, "active", ev.Status)
	case <-time.After(time.Second):
		t.Fatal("expected event delivery for matching tenant/subscription")
	}

	// Same subscription, different tenant → not delivered.
	h.Broadcast(SubscriptionEvent{SubscriptionID: "s1", TenantID: "t2", Status: "cancelled"})
	select {
	case <-client.send:
		t.Fatal("cross-tenant event must not be delivered")
	case <-time.After(50 * time.Millisecond):
	}

	// Same tenant, different subscription → not delivered.
	h.Broadcast(SubscriptionEvent{SubscriptionID: "s2", TenantID: "t1", Status: "cancelled"})
	select {
	case <-client.send:
		t.Fatal("other-subscription event must not be delivered")
	case <-time.After(50 * time.Millisecond):
	}

	h.unregister <- client
	assert.Equal(t, 0, h.ClientCount())

	// Unregistering again is a no-op.
	h.unregister <- &wsClient{tenantID: "t9", subscriptionID: "s9", send: make(chan SubscriptionEvent, 1), hub: h}
	assert.Equal(t, 0, h.ClientCount())
}

func TestWsHub_BackpressureDropsSlowClient(t *testing.T) {
	h := NewWsHub()
	defer h.Stop()

	// Buffered capacity 1: the first event is delivered, the second overflows
	// because nothing drains the queue between broadcasts.
	client := &wsClient{
		tenantID:       "t1",
		subscriptionID: "s1",
		send:           make(chan SubscriptionEvent, 1),
		hub:            h,
	}
	h.register <- client

	h.Broadcast(SubscriptionEvent{SubscriptionID: "s1", TenantID: "t1", Status: "first"})
	// Drain the single buffered slot so the next broadcast overflows.
	select {
	case ev := <-client.send:
		assert.Equal(t, "first", ev.Status)
	case <-time.After(time.Second):
		t.Fatal("expected first event")
	}

	h.Broadcast(SubscriptionEvent{SubscriptionID: "s1", TenantID: "t1", Status: "second"})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if h.ClientCount() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("slow client should have been dropped")
}

func TestWsHub_BroadcastNeverBlocks(t *testing.T) {
	h := NewWsHub()
	defer h.Stop()

	// Fill the broadcast queue so enqueue hits the timeout branch.
	for i := 0; i < wsBroadcastQueueSize; i++ {
		h.broadcast <- SubscriptionEvent{SubscriptionID: "s1", TenantID: "t1"}
	}
	old := wsBroadcastTimeout
	wsBroadcastTimeout = 20 * time.Millisecond
	defer func() { wsBroadcastTimeout = old }()

	start := time.Now()
	h.Broadcast(SubscriptionEvent{SubscriptionID: "s1", TenantID: "t1", Status: "overflow"})
	assert.Less(t, time.Since(start), time.Second)
}

func TestWsHub_StopIsIdempotent(t *testing.T) {
	h := NewWsHub()
	h.Stop()
	h.Stop() // must not panic
	assert.Equal(t, 0, h.ClientCount())
}

// ---------------------------------------------------------------------------
// Origin policy tests
// ---------------------------------------------------------------------------

func TestWsOriginPolicy_Allows(t *testing.T) {
	p := &wsOriginPolicy{}
	p.set("https://dashboard.example.com, https://admin.example.com")

	assert.True(t, p.allows("https://dashboard.example.com"))
	assert.True(t, p.allows("https://admin.example.com"))
	assert.True(t, p.allows("HTTPS://DASHBOARD.EXAMPLE.COM")) // case-insensitive
	assert.False(t, p.allows("https://evil.example.com"))
	assert.True(t, p.allows("")) // missing origin (non-browser clients)

	// Empty allow-list means allow all (development default).
	p.set("")
	assert.True(t, p.allows("https://anything.example.com"))

	// Explicit wildcard.
	p.set("*")
	assert.True(t, p.allows("https://anything.example.com"))
}

func TestConfigureWebSocketOrigins(t *testing.T) {
	old := wsOrigins
	wsOrigins = &wsOriginPolicy{}
	defer func() { wsOrigins = old }()

	ConfigureWebSocketOrigins("https://dashboard.example.com")
	assert.True(t, wsOrigins.allows("https://dashboard.example.com"))
	assert.False(t, wsOrigins.allows("https://evil.example.com"))
}

func TestWSUpgrader_CheckOrigin(t *testing.T) {
	old := wsOrigins
	wsOrigins = &wsOriginPolicy{}
	defer func() { wsOrigins = old }()

	up := newWSUpgrader()
	wsOrigins.set("https://dashboard.example.com")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/s1/events", nil)
	req.Header.Set("Origin", "https://dashboard.example.com")
	assert.True(t, up.CheckOrigin(req))

	req.Header.Set("Origin", "https://evil.example.com")
	assert.False(t, up.CheckOrigin(req))

	req.Header.Del("Origin")
	assert.True(t, up.CheckOrigin(req))
}

func TestHandler_HubAndUpgraderFallback(t *testing.T) {
	h := &Handler{}
	assert.Same(t, defaultHub, h.hub())
	assert.Same(t, defaultUpgrader, h.upgrader())

	customHub := NewWsHub()
	defer customHub.Stop()
	customUp := newWSUpgrader()
	h2 := &Handler{wsHub: customHub, wsUpgrader: customUp}
	assert.Same(t, customHub, h2.hub())
	assert.Same(t, customUp, h2.upgrader())
}

// ---------------------------------------------------------------------------
// Handler tests (non-upgrade paths)
// ---------------------------------------------------------------------------

func TestGetSubscriptionEvents_RequiresTenant(t *testing.T) {
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "sub_1"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/sub_1/events", nil)

	h.GetSubscriptionEvents(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetSubscriptionEvents_InvalidTenant(t *testing.T) {
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "sub_1"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/sub_1/events", nil)
	c.Set("tenantID", 123) // wrong type

	h.GetSubscriptionEvents(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetSubscriptionEvents_MissingID(t *testing.T) {
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions//events", nil)
	c.Set("tenantID", "t1")

	h.GetSubscriptionEvents(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetSubscriptionEvents_UpgradeFailure(t *testing.T) {
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "sub_1"}}
	// Not a WebSocket handshake (no Upgrade/Connection headers).
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/sub_1/events", nil)
	c.Set("tenantID", "t1")

	h.GetSubscriptionEvents(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// End-to-end WebSocket tests over a real httptest server
// ---------------------------------------------------------------------------

// newWSE2ETestServer spins up a Gin router that upgrades to a WebSocket for
// the subscription events route with a fixed tenant in the context.
func newWSE2ETestServer(t *testing.T) (*httptest.Server, *WsHub) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	hub := NewWsHub()
	t.Cleanup(hub.Stop)

	h := &Handler{wsHub: hub, wsUpgrader: newWSUpgrader()}

	r := gin.New()
	r.GET("/api/v1/subscriptions/:id/events", func(c *gin.Context) {
		c.Set("tenantID", "tenant-1")
		h.GetSubscriptionEvents(c)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, hub
}

func wsURL(srv *httptest.Server, subID string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/subscriptions/" + subID + "/events"
}

func dialWS(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial failed (status %d): %v", resp.StatusCode, err)
		}
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func waitForClients(t *testing.T, hub *WsHub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.ClientCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hub client count = %d, want %d", hub.ClientCount(), want)
}

func TestGetSubscriptionEvents_E2E_StreamsEvents(t *testing.T) {
	srv, hub := newWSE2ETestServer(t)
	conn := dialWS(t, wsURL(srv, "sub_1"))

	waitForClients(t, hub, 1)

	// Matching event → delivered to the client.
	hub.Broadcast(SubscriptionEvent{
		SubscriptionID: "sub_1",
		TenantID:       "tenant-1",
		Status:         "active",
		Timestamp:      "2026-08-01T00:00:00Z",
	})

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)

	var got SubscriptionEvent
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "sub_1", got.SubscriptionID)
	assert.Equal(t, "active", got.Status)
	assert.Equal(t, "tenant-1", got.TenantID)

	// Cross-tenant event → never delivered.
	hub.Broadcast(SubscriptionEvent{
		SubscriptionID: "sub_1",
		TenantID:       "tenant-2",
		Status:         "cancelled",
	})
	_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, _, err = conn.ReadMessage()
	assert.Error(t, err, "cross-tenant event must not reach the client")
}

func TestGetSubscriptionEvents_E2E_HeartbeatAndIdleClose(t *testing.T) {
	srv, hub := newWSE2ETestServer(t)
	conn := dialWS(t, wsURL(srv, "sub_1"))

	waitForClients(t, hub, 1)

	// Shorten the heartbeat/pong windows for this test.
	oldPing, oldPong, oldWrite := wsPingPeriod, wsPongWait, wsWriteWait
	wsPingPeriod, wsPongWait, wsWriteWait = 30*time.Millisecond, 100*time.Millisecond, time.Second
	defer func() {
		wsPingPeriod, wsPongWait, wsWriteWait = oldPing, oldPong, oldWrite
	}()

	// Gorilla's client automatically replies to pings with pongs, so as long
	// as the connection stays open past the pong window the heartbeat is
	// working. Give it a few ping cycles.
	time.Sleep(120 * time.Millisecond)
	assert.Equal(t, 1, hub.ClientCount(), "heartbeat should keep the client alive")

	// Now stop replying: close the connection and verify the server
	// unregisters the client (idle/close cleanup).
	_ = conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.ClientCount() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server should have unregistered the closed client")
}

func TestGetSubscriptionEvents_E2E_ClientDroppedOnBackpressure(t *testing.T) {
	srv, hub := newWSE2ETestServer(t)
	dialWS(t, wsURL(srv, "sub_1"))

	waitForClients(t, hub, 1)

	// The client never reads, so flooding events overflows its send buffer
	// and the hub must drop it instead of blocking. The client is dropped on
	// the first overflow, so a small flood is enough.
	old := wsBroadcastTimeout
	wsBroadcastTimeout = 10 * time.Millisecond
	defer func() { wsBroadcastTimeout = old }()

	start := time.Now()
	for i := 0; i < 300; i++ {
		hub.Broadcast(SubscriptionEvent{
			SubscriptionID: "sub_1",
			TenantID:       "tenant-1",
			Status:         "event",
		})
	}
	// Publishing must never block the caller.
	assert.Less(t, time.Since(start), 5*time.Second)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.ClientCount() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("slow client should have been dropped")
}

// ---------------------------------------------------------------------------
// WebSocketOutboxPublisher tests
// ---------------------------------------------------------------------------

func TestWebSocketOutboxPublisher_Publish(t *testing.T) {
	hub := NewWsHub()
	defer hub.Stop()

	p := NewWebSocketOutboxPublisher(hub).(*WebSocketOutboxPublisher)
	ctx := context.Background()

	// Non-status event types are ignored.
	require.NoError(t, p.Publish(ctx, &outbox.Event{EventType: "billing.created"}))
	require.NoError(t, p.Publish(ctx, nil))

	// Unparseable event data is ignored without error.
	require.NoError(t, p.Publish(ctx, &outbox.Event{
		EventType: subscriptionStatusChangedEvent,
		EventData: []byte("{not-json"),
	}))

	// Register a client that matches the event's tenant + subscription.
	client := &wsClient{
		tenantID:       "tenant-1",
		subscriptionID: "sub_1",
		send:           make(chan SubscriptionEvent, 8),
		hub:            hub,
	}
	hub.register <- client

	subID := "sub_1"
	ev := &outbox.Event{
		EventType:   subscriptionStatusChangedEvent,
		EventData:   []byte(`{"data":{"status":"cancelled"}}`),
		AggregateID: &subID,
		TenantID:    "tenant-1",
		OccurredAt:  time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, p.Publish(ctx, ev))

	select {
	case got := <-client.send:
		assert.Equal(t, "sub_1", got.SubscriptionID)
		assert.Equal(t, "cancelled", got.Status)
		assert.Equal(t, "tenant-1", got.TenantID)
		assert.Equal(t, "2026-08-01T12:00:00Z", got.Timestamp)
	case <-time.After(time.Second):
		t.Fatal("expected the status event to reach the hub")
	}

	// A matching event with a nil aggregate ID still forwards (empty sub id).
	require.NoError(t, p.Publish(ctx, &outbox.Event{
		EventType: subscriptionStatusChangedEvent,
		EventData: []byte(`{"data":{"status":"active"}}`),
		TenantID:  "tenant-1",
	}))
	select {
	case got := <-client.send:
		assert.Equal(t, "", got.SubscriptionID)
	case <-time.After(time.Second):
		t.Fatal("expected event with nil aggregate id")
	}
}

func TestWebSocketOutboxPublisher_DefaultHub(t *testing.T) {
	p := NewWebSocketOutboxPublisher(nil).(*WebSocketOutboxPublisher)
	assert.Same(t, defaultHub, p.hub)
}

func TestWebSocketOutboxPublisher_CrossTenantNeverDelivered(t *testing.T) {
	hub := NewWsHub()
	defer hub.Stop()

	p := NewWebSocketOutboxPublisher(hub).(*WebSocketOutboxPublisher)
	client := &wsClient{
		tenantID:       "tenant-1",
		subscriptionID: "sub_1",
		send:           make(chan SubscriptionEvent, 8),
		hub:            hub,
	}
	hub.register <- client

	subID := "sub_1"
	require.NoError(t, p.Publish(context.Background(), &outbox.Event{
		EventType:   subscriptionStatusChangedEvent,
		EventData:   []byte(`{"data":{"status":"cancelled"}}`),
		AggregateID: &subID,
		TenantID:    "tenant-2", // different tenant
	}))

	select {
	case <-client.send:
		t.Fatal("cross-tenant event must not be delivered")
	case <-time.After(50 * time.Millisecond):
	}
}

// ---------------------------------------------------------------------------
// Outbox service wiring test (NewService chains the WebSocket publisher)
// ---------------------------------------------------------------------------

type recordingPublisher struct {
	events atomic.Int32
}

func (r *recordingPublisher) Publish(context.Context, *outbox.Event) error {
	r.events.Add(1)
	return nil
}

// TestNewService_ConstructsWithWebSocketPublisher verifies the outbox service
// accepts a WebSocketPublisher in its config and constructs successfully. The
// resulting publisher chain (MultiPublisher wrapping the primary plus the WS
// publisher) is internal to the outbox package; the handler-level publisher
// bridge is exercised by the WebSocketOutboxPublisher tests above.
func TestNewService_ConstructsWithWebSocketPublisher(t *testing.T) {
	ws := &recordingPublisher{}
	svc, err := outbox.NewService(nil, outbox.ServiceConfig{
		PublisherType:     "console",
		WebSocketPublisher: ws,
	})
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Constructing with a nil WebSocketPublisher also works (default chain).
	plain, err := outbox.NewService(nil, outbox.ServiceConfig{
		PublisherType: "console",
	})
	require.NoError(t, err)
	require.NotNil(t, plain)
}
