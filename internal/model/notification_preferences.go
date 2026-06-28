type NotificationPreferences struct {
    TenantID string

    EmailEnabled bool
    SlackEnabled bool
    InAppEnabled bool

    QuietHoursEnabled bool
    QuietStart time.Time
    QuietEnd time.Time

    Timezone string

    CreatedAt time.Time
    UpdatedAt time.Time
}
