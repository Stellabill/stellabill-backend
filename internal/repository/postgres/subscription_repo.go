package postgres

import (
	"context"
	"errors"
	"stellarbill-backend/internal/metrics"
	"stellarbill-backend/internal/repository"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type pgxPool interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgx.CommandTag, error)
}

// SubscriptionRepo implements repository.SubscriptionRepository against a live Postgres database.
type SubscriptionRepo struct {
	pool pgxPool
}

// NewSubscriptionRepo constructs a SubscriptionRepo using the provided connection pool.
func NewSubscriptionRepo(pool pgxPool) *SubscriptionRepo {
	return &SubscriptionRepo{pool: pool}
}

// FindByID fetches the subscription with the given ID.
// Returns repository.ErrNotFound if no row exists.
func (r *SubscriptionRepo) FindByID(ctx context.Context, id string) (*repository.SubscriptionRow, error) {
	const q = `
		SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval, next_billing, updated_at, version, deleted_at
		FROM subscriptions
		WHERE id = $1`

	var s repository.SubscriptionRow
	var nextBilling *time.Time
	var deletedAt *time.Time

	ctx, span := tracer.Start(ctx, "SubscriptionRepo.FindByID",
		trace.WithAttributes(attribute.String("subscription.id", id)))
	defer span.End()

	timer := metrics.DBTimer("find_by_id", "subscriptions")
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&s.ID, &s.PlanID, &s.TenantID, &s.CustomerID, &s.Status,
		&s.Amount, &s.Currency, &s.Interval, &nextBilling,
		&s.UpdatedAt, &s.Version, &deletedAt,
	)
	timer(err)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	if nextBilling != nil {
		s.NextBilling = nextBilling.UTC().Format(time.RFC3339)
	}
	s.DeletedAt = deletedAt
	return &s, nil
}

// FindByIDAndTenant fetches the subscription scoped to a specific tenant.
// Returns repository.ErrNotFound if no row exists for that tenant.
func (r *SubscriptionRepo) FindByIDAndTenant(ctx context.Context, id string, tenantID string) (*repository.SubscriptionRow, error) {
	const q = `
		SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval, next_billing, updated_at, version, deleted_at
		FROM subscriptions
		WHERE id = $1 AND tenant_id = $2`

	ctx, span := tracer.Start(ctx, "SubscriptionRepo.FindByIDAndTenant",
		trace.WithAttributes(
			attribute.String("subscription.id", id),
			attribute.String("tenant.id", tenantID),
		))
	defer span.End()

	var s repository.SubscriptionRow
	var nextBilling *time.Time
	var deletedAt *time.Time

	err := r.pool.QueryRow(ctx, q, id, tenantID).Scan(
		&s.ID, &s.PlanID, &s.TenantID, &s.CustomerID, &s.Status,
		&s.Amount, &s.Currency, &s.Interval, &nextBilling,
		&s.UpdatedAt, &s.Version, &deletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	if nextBilling != nil {
		s.NextBilling = nextBilling.UTC().Format(time.RFC3339)
	}
	s.DeletedAt = deletedAt
	return &s, nil
}

// UpdateStatus updates the status of a tenant-scoped subscription.
// Returns repository.ErrNotFound if no row was updated.
func (r *SubscriptionRepo) UpdateStatus(ctx context.Context, id string, tenantID string, status string) error {
	const q = `UPDATE subscriptions SET status = $1, version = version + 1 WHERE id = $2 AND tenant_id = $3`

	ctx, span := tracer.Start(ctx, "SubscriptionRepo.UpdateStatus",
		trace.WithAttributes(
			attribute.String("subscription.id", id),
			attribute.String("tenant.id", tenantID),
			attribute.String("subscription.status", status),
		))
	defer span.End()

	tag, err := r.pool.Exec(ctx, q, status, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}
