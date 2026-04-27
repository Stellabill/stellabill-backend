package repositories

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// Plan represents a subscription plan
type Plan struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Amount      string    `json:"amount" db:"amount"`
	Currency    string    `json:"currency" db:"currency"`
	Interval    string    `json:"interval" db:"interval"`
	Description *string   `json:"description,omitempty" db:"description"`
	MerchantID  string    `json:"merchant_id" db:"merchant_id"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// PlanRepository interface for plan operations
type PlanRepository interface {
	Create(plan *Plan) error
	GetByID(id string) (*Plan, error)
	GetByMerchantID(merchantID string, limit, offset int) ([]*Plan, error)
	Update(plan *Plan) error
	Delete(id string) error
	GetActivePlansByMerchantID(merchantID string) ([]*Plan, error)
}

// postgresPlanRepository implements PlanRepository
type postgresPlanRepository struct {
	db *sql.DB
}

// NewPlanRepository creates a new plan repository
func NewPlanRepository(db *sql.DB) PlanRepository {
	return &postgresPlanRepository{db: db}
}

// Create creates a new plan
func (r *postgresPlanRepository) Create(plan *Plan) error {
	if plan.ID == "" {
		plan.ID = uuid.New().String()
	}

	query := `
		INSERT INTO plans (id, name, amount, currency, interval, description, merchant_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id
	`

	now := time.Now()
	plan.CreatedAt = now
	plan.UpdatedAt = now

	err := r.db.QueryRow(query,
		plan.ID,
		plan.Name,
		plan.Amount,
		plan.Currency,
		plan.Interval,
		plan.Description,
		plan.MerchantID,
		plan.CreatedAt,
		plan.UpdatedAt,
	).Scan(&plan.ID)

	if err != nil {
		return fmt.Errorf("failed to create plan: %w", err)
	}

	return nil
}

// GetByID retrieves a plan by ID
func (r *postgresPlanRepository) GetByID(id string) (*Plan, error) {
	query := `
		SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at
		FROM plans
		WHERE id = $1
	`

	var plan Plan
	var description sql.NullString

	err := r.db.QueryRow(query, id).Scan(
		&plan.ID,
		&plan.Name,
		&plan.Amount,
		&plan.Currency,
		&plan.Interval,
		&description,
		&plan.MerchantID,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("plan not found")
		}
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}

	if description.Valid {
		plan.Description = &description.String
	}

	return &plan, nil
}

// GetByMerchantID retrieves plans for a merchant with pagination
func (r *postgresPlanRepository) GetByMerchantID(merchantID string, limit, offset int) ([]*Plan, error) {
	query := `
		SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at
		FROM plans
		WHERE merchant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(query, merchantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get plans: %w", err)
	}
	defer rows.Close()

	var plans []*Plan
	for rows.Next() {
		plan, err := r.scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating plans: %w", err)
	}

	return plans, nil
}

// Update updates a plan
func (r *postgresPlanRepository) Update(plan *Plan) error {
	query := `
		UPDATE plans
		SET name = $1, amount = $2, currency = $3, interval = $4, description = $5, updated_at = $6
		WHERE id = $7
	`

	plan.UpdatedAt = time.Now()

	result, err := r.db.Exec(query,
		plan.Name,
		plan.Amount,
		plan.Currency,
		plan.Interval,
		plan.Description,
		plan.UpdatedAt,
		plan.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update plan: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("plan not found")
	}

	return nil
}

// Delete deletes a plan
func (r *postgresPlanRepository) Delete(id string) error {
	query := `DELETE FROM plans WHERE id = $1`

	result, err := r.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete plan: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("plan not found")
	}

	return nil
}

// GetActivePlansByMerchantID retrieves active plans for a merchant
func (r *postgresPlanRepository) GetActivePlansByMerchantID(merchantID string) ([]*Plan, error) {
	query := `
		SELECT id, name, amount, currency, interval, description, merchant_id, created_at, updated_at
		FROM plans
		WHERE merchant_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, merchantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active plans: %w", err)
	}
	defer rows.Close()

	var plans []*Plan
	for rows.Next() {
		plan, err := r.scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating active plans: %w", err)
	}

	return plans, nil
}

// scanPlan scans a database row into a Plan struct
func (r *postgresPlanRepository) scanPlan(scanner interface{ Scan(...interface{}) error }) (*Plan, error) {
	var plan Plan
	var description sql.NullString

	err := scanner.Scan(
		&plan.ID,
		&plan.Name,
		&plan.Amount,
		&plan.Currency,
		&plan.Interval,
		&description,
		&plan.MerchantID,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to scan plan: %w", err)
	}

	if description.Valid {
		plan.Description = &description.String
	}

	return &plan, nil
}

