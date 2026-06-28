type NotificationChannel interface {
    Send(ctx context.Context, event OutboxEvent) error
}
