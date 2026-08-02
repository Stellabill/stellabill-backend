package repository

import (
	"context"
	"errors"

	"stellarbill-backend/internal/requestparams"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrConcurrentUpdate is returned when a row has been modified since last read.
var ErrConcurrentUpdate = errors.New("concurrent update detected")

// SubscriptionRepository is the read interface used by the service.
type SubscriptionRepository interface {
	FindByID(ctx context.Context, id string) (*SubscriptionRow, error)
	FindByIDAndTenant(ctx context.Context, id string, tenantID string) (*SubscriptionRow, error)
	FindByIDsAndTenant(ctx context.Context, ids []string, tenantID string) ([]*SubscriptionRow, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*SubscriptionRow, error)
	UpdateStatus(ctx context.Context, id string, tenantID string, status string) error
}

// PlanRepository is the read interface used by the service.
type PlanRepository interface {
	FindByID(ctx context.Context, id string) (*PlanRow, error)
	FindByIDs(ctx context.Context, ids []string) ([]*PlanRow, error)
	FindByIDsAndTenant(ctx context.Context, ids []string, tenantID string) ([]*PlanRow, error)
	// List returns all plans visible to the caller (for simplicity tests use a global list).
	List(ctx context.Context) ([]*PlanRow, error)
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
	Filter         *requestparams.RSQLFilter
}

// StatementRepository is the read interface used by the service.
type StatementRepository interface {
	FindByID(ctx context.Context, id string) (*StatementRow, error)
	ListByCustomerID(ctx context.Context, customerID string, q StatementQuery) ([]*StatementRow, int, error)
	Create(ctx context.Context, stmt *StatementRow) error
	UpdateArchivedData(ctx context.Context, id string, stmt *StatementRow) error
}
