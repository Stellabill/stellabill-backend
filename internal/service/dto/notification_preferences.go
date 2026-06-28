type UpdateNotificationPreferencesRequest struct {
    EmailEnabled bool
    SlackEnabled bool
    InAppEnabled bool

    QuietHoursEnabled bool

    QuietStart string
    QuietEnd string

    Timezone string
}
