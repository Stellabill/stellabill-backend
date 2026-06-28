CREATE TABLE notification_preferences (
    tenant_id UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,

    email_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    slack_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    in_app_enabled BOOLEAN NOT NULL DEFAULT FALSE,

    quiet_hours_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    quiet_start TIME,
    quiet_end TIME,
    timezone TEXT NOT NULL DEFAULT 'UTC',

    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
