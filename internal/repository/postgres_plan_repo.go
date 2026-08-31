package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/repository/postgres"
	"strings"
	"time"

	"go.opentelemetry.io/otel/codes"
)

// PostgresPlanRepo implements PlanRepository using PostgreSQL via sqlc.
type PostgresPlanRepo struct {
	queries *postgres.Queries
	db      *sql.DB
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
	ctx, span := postgres.StartPlanSpan(ctx, "PlanRepo.FindByID", id)
	defer span.End()

	plan, err := r.queries.FindPlanByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.SetStatus(codes.Error, "plan not found")
			return nil, ErrNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
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

// FindByIDs fetches multiple plans by IDs in a single batch IN query.
func (r *PostgresPlanRepo) FindByIDs(ctx context.Context, ids []string) ([]*PlanRow, error) {
	return r.FindByIDsAndTenant(ctx, ids, "")
}

// FindByIDsAndTenant fetches multiple plans by IDs and optional tenantID in a single batch IN query.
func (r *PostgresPlanRepo) FindByIDsAndTenant(ctx context.Context, ids []string, tenantID string) ([]*PlanRow, error) {
	if len(ids) == 0 {
		return []*PlanRow{}, nil
	}
	if r.db == nil {
		return []*PlanRow{}, nil
	}
	args := make([]interface{}, 0, len(ids)+1)
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, id)
	}
	q := "SELECT id, tenant_id, name, amount_cents::text as amount, currency, interval, description, updated_at, version FROM plans WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	if tenantID != "" {
		args = append(args, tenantID)
		q += fmt.Sprintf(" AND tenant_id = $%d", len(args))
	}

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*PlanRow
	for rows.Next() {
		var p PlanRow
		var desc sql.NullString
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Amount, &p.Currency, &p.Interval, &desc, &p.UpdatedAt, &p.Version); err != nil {
			return nil, err
		}
		p.Description = nullableDescription(desc)
		result = append(result, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
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
