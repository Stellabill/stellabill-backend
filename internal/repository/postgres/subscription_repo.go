package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"stellarbill-backend/internal/metrics"
	"stellarbill-backend/internal/repository"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SubscriptionRepo implements repository.SubscriptionRepository against a live Postgres database.
type SubscriptionRepo struct {
	pool *pgxpool.Pool
}

// NewSubscriptionRepo constructs a SubscriptionRepo using the provided connection pool.
func NewSubscriptionRepo(pool *pgxpool.Pool) *SubscriptionRepo {
	return &SubscriptionRepo{pool: pool}
}

// FindByID fetches the subscription with the given ID.
// Returns repository.ErrNotFound if no row exists.
func (r *SubscriptionRepo) FindByID(ctx context.Context, id string) (*repository.SubscriptionRow, error) {
	const q = `
		SELECT id, plan_id, customer_id, status, amount, currency, interval, next_billing, deleted_at
		FROM subscriptions
		WHERE id = $1`

	var s repository.SubscriptionRow
	var deletedAt *time.Time

	ctx, span := tracer.Start(ctx, "SubscriptionRepo.FindByID",
		trace.WithAttributes(attribute.String("subscription.id", id)))
	defer span.End()

	timer := metrics.DBTimer("find_by_id", "subscriptions")
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&s.ID, &s.PlanID, &s.CustomerID, &s.Status,
		&s.Amount, &s.Currency, &s.Interval, &s.NextBilling,
		&deletedAt,
	)
	timer(err)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	s.DeletedAt = deletedAt
	return &s, nil
}

// FindByIDAndTenant fetches the subscription scoped to a specific tenant.
// Returns repository.ErrNotFound if no row exists for that tenant.
func (r *SubscriptionRepo) FindByIDAndTenant(ctx context.Context, id string, tenantID string) (*repository.SubscriptionRow, error) {
	const q = `
		SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval, next_billing, deleted_at
		FROM subscriptions
		WHERE id = $1 AND tenant_id = $2`

	ctx, span := tracer.Start(ctx, "SubscriptionRepo.FindByIDAndTenant",
		trace.WithAttributes(
			attribute.String("subscription.id", id),
			attribute.String("tenant.id", tenantID),
		))
	defer span.End()

	var s repository.SubscriptionRow
	var deletedAt *time.Time

	err := r.pool.QueryRow(ctx, q, id, tenantID).Scan(
		&s.ID, &s.PlanID, &s.TenantID, &s.CustomerID, &s.Status,
		&s.Amount, &s.Currency, &s.Interval, &s.NextBilling,
		&deletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	s.DeletedAt = deletedAt
	return &s, nil
}

// UpdateStatus updates the status of a tenant-scoped subscription.
// Returns repository.ErrNotFound if no row was updated.
func (r *SubscriptionRepo) UpdateStatus(ctx context.Context, id string, tenantID string, status string) error {
	const q = `UPDATE subscriptions SET status = $1 WHERE id = $2 AND tenant_id = $3`

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
