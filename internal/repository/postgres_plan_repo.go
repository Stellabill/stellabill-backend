package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"stellarbill-backend/internal/config"
)

const findPlanByIDQuery = `
	SELECT id, name, amount_cents::text, currency, interval, description
	FROM plans
	WHERE id = $1`

const listPlansQuery = `
	SELECT id, name, amount_cents::text, currency, interval, description
	FROM plans
	ORDER BY name, id`

type planDB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// PostgresPlanRepo implements PlanRepository using PostgreSQL via database/sql.
type PostgresPlanRepo struct {
	db planDB
}

var _ PlanRepository = (*PostgresPlanRepo)(nil)

// NewPostgresPlanRepo returns a PostgreSQL-backed PlanRepository.
func NewPostgresPlanRepo(db planDB) *PostgresPlanRepo {
	return &PostgresPlanRepo{db: db}
}

// ApplySQLDBPoolConfig applies validated DB_POOL_* settings to database/sql.
func ApplySQLDBPoolConfig(db *sql.DB, cfg config.Config) {
	if db == nil {
		return
	}

	db.SetMaxOpenConns(cfg.DBPoolMaxConns)
	db.SetMaxIdleConns(cfg.DBPoolMinConns)
	db.SetConnMaxLifetime(time.Duration(cfg.DBPoolMaxConnLifetime) * time.Second)
	db.SetConnMaxIdleTime(time.Duration(cfg.DBPoolMaxConnIdleTime) * time.Second)
}

// FindByID fetches a plan by ID, returning ErrNotFound when it does not exist.
func (r *PostgresPlanRepo) FindByID(ctx context.Context, id string) (*PlanRow, error) {
	var row PlanRow
	var description sql.NullString

	err := r.db.QueryRowContext(ctx, findPlanByIDQuery, id).Scan(
		&row.ID,
		&row.Name,
		&row.Amount,
		&row.Currency,
		&row.Interval,
		&description,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	row.Description = nullableDescription(description)
	return &row, nil
}

// List returns all plans ordered deterministically for stable API responses.
func (r *PostgresPlanRepo) List(ctx context.Context) ([]*PlanRow, error) {
	rows, err := r.db.QueryContext(ctx, listPlansQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	plans := make([]*PlanRow, 0)
	for rows.Next() {
		plan, err := scanPlanRow(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return plans, nil
}

func scanPlanRow(scanner interface{ Scan(dest ...any) error }) (*PlanRow, error) {
	var row PlanRow
	var description sql.NullString

	if err := scanner.Scan(
		&row.ID,
		&row.Name,
		&row.Amount,
		&row.Currency,
		&row.Interval,
		&description,
	); err != nil {
		return nil, err
	}

	row.Description = nullableDescription(description)
	return &row, nil
}

func nullableDescription(description sql.NullString) string {
	if !description.Valid {
		return ""
	}
	return description.String
}
