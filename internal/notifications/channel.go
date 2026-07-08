package notifications

import "context"

type NotificationChannel interface {
	Send(ctx context.Context, event OutboxEvent) error
}
