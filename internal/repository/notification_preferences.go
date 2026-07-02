package repository

type NotificationPreferenceRepository interface {
	GetByTenant()

	Create()

	Update()

	Upsert()
}
