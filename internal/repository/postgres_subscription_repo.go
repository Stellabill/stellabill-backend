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
               next_billing, updated_at, version, deleted_at
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
               next_billing, updated_at, version, deleted_at
        FROM subscriptions
        WHERE id = $1 AND tenant_id = $2
    `

    return r.fetchSubscription(ctx, query, id, tenantID)
}

// ListByTenant lists subscriptions for a given tenant.
func (r *PostgresSubscriptionRepo) ListByTenant(ctx context.Context, tenantID string) ([]*SubscriptionRow, error) {
    const query = `
        SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval,
               next_billing, updated_at, version, deleted_at
        FROM subscriptions
        WHERE tenant_id = $1
    `

    rows, err := r.db.QueryContext(ctx, query, tenantID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var subscriptions []*SubscriptionRow
    for rows.Next() {
        var sub SubscriptionRow
        var nextBilling sql.NullTime
        var deletedAt sql.NullTime

        err := rows.Scan(
            &sub.ID,
            &sub.PlanID,
            &sub.TenantID,
            &sub.CustomerID,
            &sub.Status,
            &sub.Amount,
            &sub.Currency,
            &sub.Interval,
            &nextBilling,
            &sub.UpdatedAt,
            &sub.Version,
            &deletedAt,
        )
        if err != nil {
            return nil, err
        }

        if nextBilling.Valid {
            sub.NextBilling = nextBilling.Time.UTC().Format(time.RFC3339)
        }
        if deletedAt.Valid {
            t := deletedAt.Time.UTC()
            sub.DeletedAt = &t
        }
        subscriptions = append(subscriptions, &sub)
    }

    return subscriptions, nil
}

// UpdateStatus updates the status for a tenant-scoped subscription record.
// It returns ErrNotFound when no record matches both id and tenant_id.
func (r *PostgresSubscriptionRepo) UpdateStatus(ctx context.Context, id string, tenantID string, status string) error {
    const query = `
        UPDATE subscriptions
        SET status = $1, version = version + 1
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

func (r *PostgresSubscriptionRepo) Update(ctx context.Context, sub *SubscriptionRow, expectedVersion int64) error {
	const query = `
		UPDATE subscriptions
		SET plan_id = $1, status = $2, amount = $3, currency = $4, interval = $5, next_billing = $6, version = version + 1
		WHERE id = $7 AND tenant_id = $8 AND version = $9
	`
	var nextBilling interface{}
	if sub.NextBilling != "" {
		nextBilling = sub.NextBilling
	}

	result, err := r.db.ExecContext(ctx, query, sub.PlanID, sub.Status, sub.Amount, sub.Currency, sub.Interval, nextBilling, sub.ID, sub.TenantID, expectedVersion)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrConcurrentUpdate
	}
	return nil
}

func (r *PostgresSubscriptionRepo) Delete(ctx context.Context, id string, tenantID string, expectedVersion int64) error {
	const query = `
		DELETE FROM subscriptions
		WHERE id = $1 AND tenant_id = $2 AND version = $3
	`
	result, err := r.db.ExecContext(ctx, query, id, tenantID, expectedVersion)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrConcurrentUpdate
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
        &subscription.UpdatedAt,
        &subscription.Version,
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
