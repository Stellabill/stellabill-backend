# WebSocket Subscription Events

`GET /api/v1/subscriptions/:id/events` streams subscription lifecycle
transitions in real time over WebSocket. Tenant dashboards can subscribe
without polling.

## Overview

- **Endpoint:** `wss://<host>/api/v1/subscriptions/:id/events`
- **Auth:** Same JWT bearer token as the REST API (route is registered behind
  the `v1` auth middleware group). The handler additionally requires a
  `tenantID` in the request context; without it the handshake is rejected with
  `401 Unauthorized`.
- **Origin validation:** The `Origin` header is checked against the configured
  allow-list (the same `ALLOWED_ORIGINS` value used by the CORS middleware,
  passed via `handlers.ConfigureWebSocketOrigins` at route registration).
  A missing `Origin` (non-browser clients) is allowed; otherwise the origin
  must be allow-listed, or the allow-list must be empty or `*` (dev default).
- **Tenant isolation:** Each connection is scoped to the authenticated
  `tenantID` and the requested subscription ID. Events are only delivered when
  both match — a tenant can never observe another tenant's transitions.

## Message format

Each event is a JSON frame:

```json
{
  "subscription_id": "sub_123",
  "status": "active",
  "timestamp": "2026-08-01T12:00:00Z",
  "tenant_id": "tenant-1"
}
```

The server sends a WebSocket **ping** every 30 seconds. Clients should reply
with a pong (browsers do this automatically). If the server does not receive a
pong within 60 seconds (idle), it closes the connection cleanly.

## Backpressure

- Each client has a bounded outbound queue (256 events).
- When a client's queue is full (a slow consumer), the hub **drops the client**
  and closes its connection instead of blocking the hub or the outbox
  dispatcher.
- The hub's inbound broadcast queue is bounded (100 events); `Broadcast` never
  blocks the caller — a full queue causes the event to be dropped and logged.

## Wiring

The outbox dispatcher publishes every `SubscriptionStatusChanged` event to the
WebSocket hub via `handlers.WebSocketOutboxPublisher`, which implements
`outbox.Publisher`:

```go
hub := handlers.NewWsHub()
wsPublisher := handlers.NewWebSocketOutboxPublisher(hub)

svc, err := outbox.NewService(db, outbox.ServiceConfig{
    PublisherType:       "multi", // or "http", "kafka", etc.
    WebSocketPublisher:  wsPublisher,
})
```

`NewService` chains the WebSocket publisher after the primary publisher (and
after any JWE wrapping) using `NewMultiPublisher`, so outbox events fan out to
both the existing publisher chain and connected WebSocket clients.

At route registration the origin allow-list is wired from config:

```go
handlers.ConfigureWebSocketOrigins(cfg.AllowedOrigins)
```

## Implementation notes

- `internal/handlers/subscription_ws.go` — hub, client pumps, handler,
  publisher bridge.
- The hub's `run` loop is the single owner of the client map; all mutations
  happen under its mutex, so there are no concurrent map accesses.
- `writePump` enforces a 10s write deadline; `readPump` caps inbound frames at
  512 bytes (clients are read-only).
- Timing values (`wsPingPeriod`, `wsPongWait`, `wsWriteWait`,
  `wsBroadcastTimeout`) are variables so tests can shorten them; production
  defaults implement the required 30s heartbeat / 60s idle tolerance.

## Testing

```sh
go test ./internal/handlers/... -run 'Subscription|WsHub|WebSocket|Origin|GetSubscriptionEvents' -v
go test ./internal/handlers/... -coverprofile=cover.out
go tool cover -func=cover.out | grep subscription_ws
```
