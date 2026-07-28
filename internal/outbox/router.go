package outbox

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

type NotificationRouter struct {
	email NotificationChannel
	slack NotificationChannel
	inApp NotificationChannel

	prefs PreferenceRepository
}

type PreferenceRepository interface{}
