package outbox

import "context"

type NotificationChannel interface {
	Send(ctx context.Context, event Event) error
}

type NotificationRouter struct {
	email NotificationChannel
	slack NotificationChannel
	inApp NotificationChannel
	prefs PreferenceRepository
}

type PreferenceRepository interface{}
