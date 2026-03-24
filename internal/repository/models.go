package repository

import "time"

// SubscriptionRow is the raw DB record for a subscription.
type SubscriptionRow struct {
	ID          string
	PlanID      string
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
