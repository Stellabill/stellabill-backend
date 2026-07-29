package repository

import (
	"context"
	"database/sql"
	"errors"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/repository/postgres"
	"time"
)

// PostgresPlanRepo implements PlanRepository using PostgreSQL via sqlc.
type PostgresPlanRepo struct {
	queries *postgres.Queries
	db      *sql.DB
}

// PostgresPlanRepo implements PlanRepository using PostgreSQL via database/sql.
type PostgresPlanRepo struct {
	db planDB
}

var _ PlanRepository = (*PostgresPlanRepo)(nil)

// NewPostgresPlanRepo returns a PostgreSQL-backed PlanRepository.
func NewPostgresPlanRepo(db *sql.DB) *PostgresPlanRepo {
	return &PostgresPlanRepo{
		queries: postgres.New(db),
		db:      db,
	}
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
	plan, err := r.queries.FindPlanByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &PlanRow{
		ID:          plan.ID,
		TenantID:    plan.TenantID,
		Name:        plan.Name,
		Amount:      plan.Amount,
		Currency:    plan.Currency,
		Interval:    plan.Interval,
		Description: nullableDescription(plan.Description),
		UpdatedAt:   plan.UpdatedAt,
		Version:     plan.Version,
	}, nil
}

// List returns all plans ordered deterministically for stable API responses.
func (r *PostgresPlanRepo) List(ctx context.Context) ([]*PlanRow, error) {
	plans, err := r.queries.ListPlans(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*PlanRow, len(plans))
	for i, plan := range plans {
		result[i] = &PlanRow{
			ID:          plan.ID,
			TenantID:    plan.TenantID,
			Name:        plan.Name,
			Amount:      plan.Amount,
			Currency:    plan.Currency,
			Interval:    plan.Interval,
			Description: nullableDescription(plan.Description),
			UpdatedAt:   plan.UpdatedAt,
			Version:     plan.Version,
		}
	}

	return result, nil
}

func (r *PostgresPlanRepo) Update(ctx context.Context, plan *PlanRow, expectedVersion int64) error {
	affected, err := r.queries.UpdatePlan(ctx, postgres.UpdatePlanParams{
		Name:        plan.Name,
		Description: sql.NullString{String: plan.Description, Valid: plan.Description != ""},
		ID:          plan.ID,
		Version:     expectedVersion,
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConcurrentUpdate
	}
	return nil
}

func (r *PostgresPlanRepo) Delete(ctx context.Context, id string, expectedVersion int64) error {
	affected, err := r.queries.DeletePlan(ctx, postgres.DeletePlanParams{
		ID:      id,
		Version: expectedVersion,
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConcurrentUpdate
	}
	return nil
}

func nullableDescription(description sql.NullString) string {
	if !description.Valid {
		return ""
	}
	return description.String
}
