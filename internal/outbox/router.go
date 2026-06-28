type NotificationChannel interface {
    Send(ctx context.Context, event OutboxEvent) error
}

type NotificationRouter struct {
    email NotificationChannel
    slack NotificationChannel
    inApp NotificationChannel

    prefs PreferenceRepository
}
