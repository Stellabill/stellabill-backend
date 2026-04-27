package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"stellarbill-backend/internal/repository"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var planTracer = otel.Tracer("repository/postgres")

// PlanRepo implements repository.PlanRepository against a live Postgres database.
type PlanRepo struct {
	pool *pgxpool.Pool
}

// NewPlanRepo constructs a PlanRepo using the provided connection pool.
func NewPlanRepo(pool *pgxpool.Pool) *PlanRepo {
	return &PlanRepo{pool: pool}
}

// FindByID fetches the plan with the given ID.
// Returns repository.ErrNotFound if no row exists.
func (r *PlanRepo) FindByID(ctx context.Context, id string) (*repository.PlanRow, error) {
	const q = `
		SELECT id, name, amount, currency, interval, description
		FROM plans
		WHERE id = $1`

	var p repository.PlanRow
	ctx, span := planTracer.Start(ctx, "PlanRepo.FindByID",
		trace.WithAttributes(attribute.String("plan.id", id)))
	defer span.End()

	err := r.pool.QueryRow(ctx, q, id).
		Scan(&p.ID, &p.Name, &p.Amount, &p.Currency, &p.Interval, &p.Description)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

// List returns all plans ordered by id.
func (r *PlanRepo) List(ctx context.Context) ([]*repository.PlanRow, error) {
	const q = `
		SELECT id, name, amount, currency, interval, description
		FROM plans
		ORDER BY id ASC`

	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	plans := make([]*repository.PlanRow, 0)
	for rows.Next() {
		p := &repository.PlanRow{}
		if err := rows.Scan(&p.ID, &p.Name, &p.Amount, &p.Currency, &p.Interval, &p.Description); err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return plans, nil
}
