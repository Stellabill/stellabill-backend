package repository

import (
    "context"
    "database/sql"
    "time"
)

// PostgresSubscriptionRepo is a PostgreSQL-backed SubscriptionRepository.
// It uses database/sql to execute queries against a subscriptions table and
// maps nullable timestamps into the internal SubscriptionRow model.
type PostgresSubscriptionRepo struct {
    db *sql.DB
}

// NewPostgresSubscriptionRepo constructs a new PostgresSubscriptionRepo.
func NewPostgresSubscriptionRepo(db *sql.DB) *PostgresSubscriptionRepo {
    return &PostgresSubscriptionRepo{db: db}
}

// FindByID queries subscriptions by id only.
// It returns ErrNotFound when there is no matching record.
func (r *PostgresSubscriptionRepo) FindByID(ctx context.Context, id string) (*SubscriptionRow, error) {
    const query = `
        SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval,
               next_billing, deleted_at
        FROM subscriptions
        WHERE id = $1
    `

    return r.fetchSubscription(ctx, query, id)
}

// FindByIDAndTenant queries subscriptions by id and tenant_id in SQL.
// Tenant isolation is enforced in the database predicate, not in Go.
func (r *PostgresSubscriptionRepo) FindByIDAndTenant(ctx context.Context, id string, tenantID string) (*SubscriptionRow, error) {
    const query = `
        SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval,
               next_billing, deleted_at
        FROM subscriptions
        WHERE id = $1 AND tenant_id = $2
    `

    return r.fetchSubscription(ctx, query, id, tenantID)
}

// UpdateStatus updates the status for a tenant-scoped subscription record.
// It returns ErrNotFound when no record matches both id and tenant_id.
func (r *PostgresSubscriptionRepo) UpdateStatus(ctx context.Context, id string, tenantID string, status string) error {
    const query = `
        UPDATE subscriptions
        SET status = $1
        WHERE id = $2 AND tenant_id = $3
    `

    result, err := r.db.ExecContext(ctx, query, status, id, tenantID)
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

func (r *PostgresSubscriptionRepo) fetchSubscription(ctx context.Context, query string, args ...any) (*SubscriptionRow, error) {
    row := r.db.QueryRowContext(ctx, query, args...)

    var subscription SubscriptionRow
    var nextBilling sql.NullTime
    var deletedAt sql.NullTime

    err := row.Scan(
        &subscription.ID,
        &subscription.PlanID,
        &subscription.TenantID,
        &subscription.CustomerID,
        &subscription.Status,
        &subscription.Amount,
        &subscription.Currency,
        &subscription.Interval,
        &nextBilling,
        &deletedAt,
    )
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, ErrNotFound
        }
        return nil, err
    }

    if nextBilling.Valid {
        subscription.NextBilling = nextBilling.Time.UTC().Format(time.RFC3339)
    }
    if deletedAt.Valid {
        t := deletedAt.Time.UTC()
        subscription.DeletedAt = &t
    }

    return &subscription, nil
}
