package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PostgresPlanTemplateRepo implements PlanTemplateRepository using PostgreSQL.
type PostgresPlanTemplateRepo struct {
	db planDB // reuse the same interface from plan repo
}

var _ PlanTemplateRepository = (*PostgresPlanTemplateRepo)(nil)

// NewPostgresPlanTemplateRepo creates a new PostgreSQL-backed plan template repository.
func NewPostgresPlanTemplateRepo(db planDB) *PostgresPlanTemplateRepo {
	return &PostgresPlanTemplateRepo{db: db}
}

// Create inserts a new plan template.
func (r *PostgresPlanTemplateRepo) Create(ctx context.Context, template *PlanTemplateRow) error {
	const query = `
		INSERT INTO plan_templates (
			id, merchant_id, name, amount_cents, currency, 
			interval_seconds, trial_seconds, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, query,
		template.ID,
		template.MerchantID,
		template.Name,
		template.AmountCents,
		template.Currency,
		template.IntervalSeconds,
		template.TrialSeconds,
		now,
		now,
	)
	
	return err
}

// FindByID fetches a plan template by ID.
func (r *PostgresPlanTemplateRepo) FindByID(ctx context.Context, id string) (*PlanTemplateRow, error) {
	const query = `
		SELECT id, merchant_id, name, amount_cents, currency, 
		       interval_seconds, trial_seconds, deprecated_at, 
		       created_at, updated_at
		FROM plan_templates
		WHERE id = $1
	`
	
	var template PlanTemplateRow
	var deprecatedAt sql.NullTime
	
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&template.ID,
		&template.MerchantID,
		&template.Name,
		&template.AmountCents,
		&template.Currency,
		&template.IntervalSeconds,
		&template.TrialSeconds,
		&deprecatedAt,
		&template.CreatedAt,
		&template.UpdatedAt,
	)
	
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	
	if deprecatedAt.Valid {
		template.DeprecatedAt = &deprecatedAt.Time
	}
	
	return &template, nil
}

// FindByMerchantAndName fetches a plan template by merchant ID and name.
func (r *PostgresPlanTemplateRepo) FindByMerchantAndName(ctx context.Context, merchantID string, name string) (*PlanTemplateRow, error) {
	const query = `
		SELECT id, merchant_id, name, amount_cents, currency, 
		       interval_seconds, trial_seconds, deprecated_at, 
		       created_at, updated_at
		FROM plan_templates
		WHERE merchant_id = $1 AND name = $2
	`
	
	var template PlanTemplateRow
	var deprecatedAt sql.NullTime
	
	err := r.db.QueryRowContext(ctx, query, merchantID, name).Scan(
		&template.ID,
		&template.MerchantID,
		&template.Name,
		&template.AmountCents,
		&template.Currency,
		&template.IntervalSeconds,
		&template.TrialSeconds,
		&deprecatedAt,
		&template.CreatedAt,
		&template.UpdatedAt,
	)
	
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	
	if deprecatedAt.Valid {
		template.DeprecatedAt = &deprecatedAt.Time
	}
	
	return &template, nil
}

// ListByMerchant returns all plan templates for a merchant.
func (r *PostgresPlanTemplateRepo) ListByMerchant(ctx context.Context, merchantID string, includeDeprecated bool) ([]*PlanTemplateRow, error) {
	var query string
	if includeDeprecated {
		query = `
			SELECT id, merchant_id, name, amount_cents, currency, 
			       interval_seconds, trial_seconds, deprecated_at, 
			       created_at, updated_at
			FROM plan_templates
			WHERE merchant_id = $1
			ORDER BY created_at DESC
		`
	} else {
		query = `
			SELECT id, merchant_id, name, amount_cents, currency, 
			       interval_seconds, trial_seconds, deprecated_at, 
			       created_at, updated_at
			FROM plan_templates
			WHERE merchant_id = $1 AND deprecated_at IS NULL
			ORDER BY created_at DESC
		`
	}
	
	rows, err := r.db.QueryContext(ctx, query, merchantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var templates []*PlanTemplateRow
	for rows.Next() {
		var template PlanTemplateRow
		var deprecatedAt sql.NullTime
		
		if err := rows.Scan(
			&template.ID,
			&template.MerchantID,
			&template.Name,
			&template.AmountCents,
			&template.Currency,
			&template.IntervalSeconds,
			&template.TrialSeconds,
			&deprecatedAt,
			&template.CreatedAt,
			&template.UpdatedAt,
		); err != nil {
			return nil, err
		}
		
		if deprecatedAt.Valid {
			template.DeprecatedAt = &deprecatedAt.Time
		}
		
		templates = append(templates, &template)
	}
	
	if err := rows.Err(); err != nil {
		return nil, err
	}
	
	return templates, nil
}

// Deprecate marks a plan template as deprecated.
func (r *PostgresPlanTemplateRepo) Deprecate(ctx context.Context, id string, merchantID string) error {
	const query = `
		UPDATE plan_templates
		SET deprecated_at = $1
		WHERE id = $2 AND merchant_id = $3 AND deprecated_at IS NULL
	`
	
	result, err := r.db.ExecContext(ctx, query, time.Now().UTC(), id, merchantID)
	if err != nil {
		return err
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	
	if rowsAffected == 0 {
		return ErrNotFound
	}
	
	return nil
}
