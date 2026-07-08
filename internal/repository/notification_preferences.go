package repository

import "context"

type NotificationPreferenceRepository interface {
	GetByTenant(ctx context.Context, tenantID string) (*NotificationPreferenceRow, error)
	Create(ctx context.Context, pref *NotificationPreferenceRow) error
	Update(ctx context.Context, pref *NotificationPreferenceRow) error
	Upsert(ctx context.Context, pref *NotificationPreferenceRow) error
}

type NotificationPreferenceRow struct {
	TenantID string
}
