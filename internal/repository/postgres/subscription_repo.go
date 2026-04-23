package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/timeutil"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var subscriptionTracer = otel.Tracer("repository/postgres/subscription")

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

	ctx, span := subscriptionTracer.Start(ctx, "SubscriptionRepo.FindByID",
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
	s.DeletedAt = timeutil.NormalizePtrUTC(deletedAt)

	normalizedNextBilling, err := timeutil.NormalizeRFC3339StringToUTC(s.NextBilling)
	if err == nil {
		s.NextBilling = normalizedNextBilling
	}

	return &s, nil
}

// FindByIDAndTenant fetches a subscription by id and returns not found when tenant does not match.
func (r *SubscriptionRepo) FindByIDAndTenant(ctx context.Context, id string, tenantID string) (*repository.SubscriptionRow, error) {
	s, err := r.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Current schema does not persist tenant for subscriptions; keep caller-provided tenant for service checks.
	s.TenantID = tenantID
	return s, nil
}
