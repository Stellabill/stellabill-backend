package repository

import "time"

// SubscriptionRow is the raw DB record for a subscription.
type SubscriptionRow struct {
	ID          string
	PlanID      string
	TenantID    string // tenant isolation boundary
	CustomerID  string // used for ownership check; NOT exposed in response
	Status      string
	Amount      string // e.g. "1999" (cents as string) or "19.99"
	Currency    string // ISO 4217
	Interval    string
	NextBilling string // RFC 3339 or empty
	DeletedAt   *time.Time
}

// PlanRow is the raw DB record for a billing plan.
type PlanRow struct {
	ID          string
	Name        string
	Amount      string
	Currency    string
	Interval    string
	Description string
}

// StatementRow is the raw DB record for a billing statement.
type StatementRow struct {
	ID             string
	SubscriptionID string
	CustomerID     string
	PeriodStart    string // RFC 3339 (NULL if archived)
	PeriodEnd      string // RFC 3339 (NULL if archived)
	IssuedAt       string // RFC 3339 (NULL if archived)
	TotalAmount    string // NULL if archived
	Currency       string // NULL if archived
	Kind           string // NULL if archived
	Status         string // NULL if archived
	DeletedAt      *time.Time
	ArchivedAt     *time.Time // timestamp when statement was archived
	ArchiveKey     string     // S3-like path to archived data (only set if archived)
}
