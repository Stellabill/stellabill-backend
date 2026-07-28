package repository

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrConcurrentUpdate is returned when a concurrent update prevents modification.
var ErrConcurrentUpdate = errors.New("concurrent update")

// SubscriptionRepository is the read interface used by the service.
type SubscriptionRepository interface {
	FindByID(ctx context.Context, id string) (*SubscriptionRow, error)
	FindByIDAndTenant(ctx context.Context, id string, tenantID string) (*SubscriptionRow, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*SubscriptionRow, error)
	UpdateStatus(ctx context.Context, id string, tenantID string, status string) error
	Update(ctx context.Context, sub *SubscriptionRow, expectedVersion int64) error
	Delete(ctx context.Context, id string, tenantID string, expectedVersion int64) error
}

// PlanRepository is the read interface used by the service.
type PlanRepository interface {
	FindByID(ctx context.Context, id string) (*PlanRow, error)
	// List returns all plans visible to the caller (for simplicity tests use a global list).
	List(ctx context.Context) ([]*PlanRow, error)
	Update(ctx context.Context, plan *PlanRow, expectedVersion int64) error
	Delete(ctx context.Context, id string, expectedVersion int64) error
}

// StatementQuery defines the parameters for listing statements.
type StatementQuery struct {
	SubscriptionID string
	Kind           string
	Status         string
	StartAfter     string
	EndBefore      string
	StartingAfter  string // cursor for forward pagination
	EndingBefore   string // cursor for backward pagination
	Limit          int    // replaces PageSize
	Order          string // e.g. "asc", "desc"
}

// StatementRepository is the read interface used by the service.
type StatementRepository interface {
	FindByID(ctx context.Context, id string) (*StatementRow, error)
	ListByCustomerID(ctx context.Context, customerID string, q StatementQuery) ([]*StatementRow, int, error)
	Create(ctx context.Context, stmt *StatementRow) error
	UpdateArchivedData(ctx context.Context, id string, stmt *StatementRow) error
}
