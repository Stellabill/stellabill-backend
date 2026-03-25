package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"stellarbill-backend/internal/repository"
)

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
