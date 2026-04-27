package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"stellabill-backend/internal/repository"
)

var tracer = otel.Tracer("repository/postgres")

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

	err := r.pool.QueryRow(ctx, q, id).Scan(
		&s.ID, &s.PlanID, &s.CustomerID, &s.Status,
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

// FindByIDAndTenant fetches the subscription with the given ID.
// For now, tenant is ignored as the table does not have tenant column.
// Returns repository.ErrNotFound if no row exists.
func (r *SubscriptionRepo) FindByIDAndTenant(ctx context.Context, id string, tenantID string) (*repository.SubscriptionRow, error) {
	return r.FindByID(ctx, id)
}

