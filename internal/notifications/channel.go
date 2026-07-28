package notifications

import "context"

// OutboxEvent is a generic outbox event payload.
type OutboxEvent struct {
	ID      string
	Type    string
	Payload []byte
}

type NotificationChannel interface {
	Send(ctx context.Context, event OutboxEvent) error
}
