package repositories

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// Subscription represents a customer subscription
type Subscription struct {
	ID                 string     `json:"id" db:"id"`
	PlanID             string     `json:"plan_id" db:"plan_id"`
	CustomerID         string     `json:"customer_id" db:"customer_id"`
	MerchantID         string     `json:"merchant_id" db:"merchant_id"`
	Status             string     `json:"status" db:"status"`
	Amount             string     `json:"amount" db:"amount"`
	Currency           string     `json:"currency" db:"currency"`
	Interval           string     `json:"interval" db:"interval"`
	CurrentPeriodStart time.Time  `json:"current_period_start" db:"current_period_start"`
	CurrentPeriodEnd   time.Time  `json:"current_period_end" db:"current_period_end"`
	CancelAtPeriodEnd  bool       `json:"cancel_at_period_end" db:"cancel_at_period_end"`
	CanceledAt         *time.Time `json:"canceled_at,omitempty" db:"canceled_at"`
	EndedAt            *time.Time `json:"ended_at,omitempty" db:"ended_at"`
	TrialStart         *time.Time `json:"trial_start,omitempty" db:"trial_start"`
	TrialEnd           *time.Time `json:"trial_end,omitempty" db:"trial_end"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
}

// SubscriptionRepository interface for subscription operations
type SubscriptionRepository interface {
	Create(subscription *Subscription) error
	GetByID(id string) (*Subscription, error)
	GetByCustomerID(customerID string, limit, offset int) ([]*Subscription, error)
	GetByMerchantID(merchantID string, limit, offset int) ([]*Subscription, error)
	GetByPlanID(planID string, limit, offset int) ([]*Subscription, error)
	Update(subscription *Subscription) error
	UpdateStatus(id string, status string) error
	Cancel(id string, cancelAtPeriodEnd bool) error
	GetActiveSubscriptionsByMerchantID(merchantID string) ([]*Subscription, error)
	GetSubscriptionsDueForBilling(limit int) ([]*Subscription, error)
}

// postgresSubscriptionRepository implements SubscriptionRepository
type postgresSubscriptionRepository struct {
	db *sql.DB
}

// NewSubscriptionRepository creates a new subscription repository
func NewSubscriptionRepository(db *sql.DB) SubscriptionRepository {
	return &postgresSubscriptionRepository{db: db}
}

// Create creates a new subscription
func (r *postgresSubscriptionRepository) Create(subscription *Subscription) error {
	if subscription.ID == "" {
		subscription.ID = uuid.New().String()
	}

	query := `
		INSERT INTO subscriptions (
			id, plan_id, customer_id, merchant_id, status, amount, currency, interval,
			current_period_start, current_period_end, cancel_at_period_end,
			canceled_at, ended_at, trial_start, trial_end, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING id
	`

	now := time.Now()
	subscription.CreatedAt = now
	subscription.UpdatedAt = now

	err := r.db.QueryRow(query,
		subscription.ID,
		subscription.PlanID,
		subscription.CustomerID,
		subscription.MerchantID,
		subscription.Status,
		subscription.Amount,
		subscription.Currency,
		subscription.Interval,
		subscription.CurrentPeriodStart,
		subscription.CurrentPeriodEnd,
		subscription.CancelAtPeriodEnd,
		subscription.CanceledAt,
		subscription.EndedAt,
		subscription.TrialStart,
		subscription.TrialEnd,
		subscription.CreatedAt,
		subscription.UpdatedAt,
	).Scan(&subscription.ID)

	if err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	return nil
}

// GetByID retrieves a subscription by ID
func (r *postgresSubscriptionRepository) GetByID(id string) (*Subscription, error) {
	query := `
		SELECT id, plan_id, customer_id, merchant_id, status, amount, currency, interval,
			   current_period_start, current_period_end, cancel_at_period_end,
			   canceled_at, ended_at, trial_start, trial_end, created_at, updated_at
		FROM subscriptions
		WHERE id = $1
	`

	var subscription Subscription
	var canceledAt, endedAt, trialStart, trialEnd sql.NullTime

	err := r.db.QueryRow(query, id).Scan(
		&subscription.ID,
		&subscription.PlanID,
		&subscription.CustomerID,
		&subscription.MerchantID,
		&subscription.Status,
		&subscription.Amount,
		&subscription.Currency,
		&subscription.Interval,
		&subscription.CurrentPeriodStart,
		&subscription.CurrentPeriodEnd,
		&subscription.CancelAtPeriodEnd,
		&canceledAt,
		&endedAt,
		&trialStart,
		&trialEnd,
		&subscription.CreatedAt,
		&subscription.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("subscription not found")
		}
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	if canceledAt.Valid {
		subscription.CanceledAt = &canceledAt.Time
	}

	if endedAt.Valid {
		subscription.EndedAt = &endedAt.Time
	}

	if trialStart.Valid {
		subscription.TrialStart = &trialStart.Time
	}

	if trialEnd.Valid {
		subscription.TrialEnd = &trialEnd.Time
	}

	return &subscription, nil
}

// GetByCustomerID retrieves subscriptions for a customer with pagination
func (r *postgresSubscriptionRepository) GetByCustomerID(customerID string, limit, offset int) ([]*Subscription, error) {
	query := `
		SELECT id, plan_id, customer_id, merchant_id, status, amount, currency, interval,
			   current_period_start, current_period_end, cancel_at_period_end,
			   canceled_at, ended_at, trial_start, trial_end, created_at, updated_at
		FROM subscriptions
		WHERE customer_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(query, customerID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions: %w", err)
	}
	defer rows.Close()

	var subscriptions []*Subscription
	for rows.Next() {
		subscription, err := r.scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating subscriptions: %w", err)
	}

	return subscriptions, nil
}

// GetByMerchantID retrieves subscriptions for a merchant with pagination
func (r *postgresSubscriptionRepository) GetByMerchantID(merchantID string, limit, offset int) ([]*Subscription, error) {
	query := `
		SELECT id, plan_id, customer_id, merchant_id, status, amount, currency, interval,
			   current_period_start, current_period_end, cancel_at_period_end,
			   canceled_at, ended_at, trial_start, trial_end, created_at, updated_at
		FROM subscriptions
		WHERE merchant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(query, merchantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions: %w", err)
	}
	defer rows.Close()

	var subscriptions []*Subscription
	for rows.Next() {
		subscription, err := r.scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating subscriptions: %w", err)
	}

	return subscriptions, nil
}

// GetByPlanID retrieves subscriptions for a plan with pagination
func (r *postgresSubscriptionRepository) GetByPlanID(planID string, limit, offset int) ([]*Subscription, error) {
	query := `
		SELECT id, plan_id, customer_id, merchant_id, status, amount, currency, interval,
			   current_period_start, current_period_end, cancel_at_period_end,
			   canceled_at, ended_at, trial_start, trial_end, created_at, updated_at
		FROM subscriptions
		WHERE plan_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(query, planID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions: %w", err)
	}
	defer rows.Close()

	var subscriptions []*Subscription
	for rows.Next() {
		subscription, err := r.scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating subscriptions: %w", err)
	}

	return subscriptions, nil
}

// Update updates a subscription
func (r *postgresSubscriptionRepository) Update(subscription *Subscription) error {
	query := `
		UPDATE subscriptions
		SET status = $1, current_period_start = $2, current_period_end = $3,
			cancel_at_period_end = $4, canceled_at = $5, ended_at = $6,
			trial_start = $7, trial_end = $8, updated_at = $9
		WHERE id = $10
	`

	subscription.UpdatedAt = time.Now()

	result, err := r.db.Exec(query,
		subscription.Status,
		subscription.CurrentPeriodStart,
		subscription.CurrentPeriodEnd,
		subscription.CancelAtPeriodEnd,
		subscription.CanceledAt,
		subscription.EndedAt,
		subscription.TrialStart,
		subscription.TrialEnd,
		subscription.UpdatedAt,
		subscription.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("subscription not found")
	}

	return nil
}

// UpdateStatus updates subscription status
func (r *postgresSubscriptionRepository) UpdateStatus(id string, status string) error {
	query := `
		UPDATE subscriptions
		SET status = $1, updated_at = $2
		WHERE id = $3
	`

	result, err := r.db.Exec(query, status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update subscription status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("subscription not found")
	}

	return nil
}

// Cancel cancels a subscription
func (r *postgresSubscriptionRepository) Cancel(id string, cancelAtPeriodEnd bool) error {
	query := `
		UPDATE subscriptions
		SET cancel_at_period_end = $1, canceled_at = $2, updated_at = $3
		WHERE id = $4
	`

	now := time.Now()

	result, err := r.db.Exec(query, cancelAtPeriodEnd, &now, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to cancel subscription: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("subscription not found")
	}

	return nil
}

// GetActiveSubscriptionsByMerchantID retrieves active subscriptions for a merchant
func (r *postgresSubscriptionRepository) GetActiveSubscriptionsByMerchantID(merchantID string) ([]*Subscription, error) {
	query := `
		SELECT id, plan_id, customer_id, merchant_id, status, amount, currency, interval,
			   current_period_start, current_period_end, cancel_at_period_end,
			   canceled_at, ended_at, trial_start, trial_end, created_at, updated_at
		FROM subscriptions
		WHERE merchant_id = $1 AND status IN ('active', 'trialing')
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(query, merchantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get active subscriptions: %w", err)
	}
	defer rows.Close()

	var subscriptions []*Subscription
	for rows.Next() {
		subscription, err := r.scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating active subscriptions: %w", err)
	}

	return subscriptions, nil
}

// GetSubscriptionsDueForBilling retrieves subscriptions that need billing
func (r *postgresSubscriptionRepository) GetSubscriptionsDueForBilling(limit int) ([]*Subscription, error) {
	query := `
		SELECT id, plan_id, customer_id, merchant_id, status, amount, currency, interval,
			   current_period_start, current_period_end, cancel_at_period_end,
			   canceled_at, ended_at, trial_start, trial_end, created_at, updated_at
		FROM subscriptions
		WHERE status IN ('active', 'trialing') 
		  AND current_period_end <= $1
		  AND (cancel_at_period_end = false OR canceled_at IS NULL)
		ORDER BY current_period_end ASC
		LIMIT $2
	`

	rows, err := r.db.Query(query, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions due for billing: %w", err)
	}
	defer rows.Close()

	var subscriptions []*Subscription
	for rows.Next() {
		subscription, err := r.scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating subscriptions due for billing: %w", err)
	}

	return subscriptions, nil
}

// scanSubscription scans a database row into a Subscription struct
func (r *postgresSubscriptionRepository) scanSubscription(scanner interface{ Scan(...interface{}) error }) (*Subscription, error) {
	var subscription Subscription
	var canceledAt, endedAt, trialStart, trialEnd sql.NullTime

	err := scanner.Scan(
		&subscription.ID,
		&subscription.PlanID,
		&subscription.CustomerID,
		&subscription.MerchantID,
		&subscription.Status,
		&subscription.Amount,
		&subscription.Currency,
		&subscription.Interval,
		&subscription.CurrentPeriodStart,
		&subscription.CurrentPeriodEnd,
		&subscription.CancelAtPeriodEnd,
		&canceledAt,
		&endedAt,
		&trialStart,
		&trialEnd,
		&subscription.CreatedAt,
		&subscription.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to scan subscription: %w", err)
	}

	if canceledAt.Valid {
		subscription.CanceledAt = &canceledAt.Time
	}

	if endedAt.Valid {
		subscription.EndedAt = &endedAt.Time
	}

	if trialStart.Valid {
		subscription.TrialStart = &trialStart.Time
	}

	if trialEnd.Valid {
		subscription.TrialEnd = &trialEnd.Time
	}

	return &subscription, nil
}

