package postgres

import (
	"context"
	"errors"
	"stellarbill-backend/internal/metrics"
	"stellarbill-backend/internal/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var planTracer = otel.Tracer("repository/postgres")

type pgxQueryRow interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgx.CommandTag, error)
}

// PlanRepo implements repository.PlanRepository against a live Postgres database.
type PlanRepo struct {
	pool pgxQueryRow
}

// NewPlanRepo constructs a PlanRepo using the provided connection pool.
func NewPlanRepo(pool pgxQueryRow) *PlanRepo {
	return &PlanRepo{pool: pool}
}

// FindByID fetches the plan with the given ID.
// Returns repository.ErrNotFound if no row exists.
func (r *PlanRepo) FindByID(ctx context.Context, id string) (*repository.PlanRow, error) {
	const q = `
		SELECT id, tenant_id, name, amount_cents::text as amount, currency, interval, description, updated_at, version
		FROM plans
		WHERE id = $1`

	var p repository.PlanRow
	ctx, span := planTracer.Start(ctx, "PlanRepo.FindByID",
		trace.WithAttributes(attribute.String("plan.id", id)))
	defer span.End()

	timer := metrics.DBTimer("find_by_id", "plans")
	err := r.pool.QueryRow(ctx, q, id).
		Scan(&p.ID, &p.TenantID, &p.Name, &p.Amount, &p.Currency, &p.Interval, &p.Description, &p.UpdatedAt, &p.Version)
	timer(err)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}
