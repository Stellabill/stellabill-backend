package repository

import (
	"context"
	"database/sql"
	"errors"
	"stellarbill-backend/internal/config"
	"time"
)

const findPlanByIDQuery = `
	SELECT id, tenant_id, name, amount_cents::text, currency, interval, description, updated_at, version
	FROM plans
	WHERE id = $1`

const listPlansQuery = `
	SELECT id, tenant_id, name, amount_cents::text, currency, interval, description, updated_at, version
	FROM plans
	ORDER BY name, id`

type planDB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
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
		&row.TenantID,
		&row.Name,
		&row.Amount,
		&row.Currency,
		&row.Interval,
		&description,
		&row.UpdatedAt,
		&row.Version,
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

func (r *PostgresPlanRepo) Update(ctx context.Context, plan *PlanRow, expectedVersion int64) error {
	const query = `
		UPDATE plans
		SET name = $1, description = $2, version = version + 1
		WHERE id = $3 AND version = $4
	`
	res, err := r.db.ExecContext(ctx, query, plan.Name, plan.Description, plan.ID, expectedVersion)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConcurrentUpdate
	}
	return nil
}

func (r *PostgresPlanRepo) Delete(ctx context.Context, id string, expectedVersion int64) error {
	const query = `
		DELETE FROM plans
		WHERE id = $1 AND version = $2
	`
	res, err := r.db.ExecContext(ctx, query, id, expectedVersion)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConcurrentUpdate
	}
	return nil
}

func scanPlanRow(scanner interface{ Scan(dest ...any) error }) (*PlanRow, error) {
	var row PlanRow
	var description sql.NullString

	if err := scanner.Scan(
		&row.ID,
		&row.TenantID,
		&row.Name,
		&row.Amount,
		&row.Currency,
		&row.Interval,
		&description,
		&row.UpdatedAt,
		&row.Version,
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
