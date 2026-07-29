package repository

import (
	"context"
	"database/sql"
	"stellarbill-backend/internal/repository/postgres"
	"time"
)

// PostgresSubscriptionRepo is a PostgreSQL-backed SubscriptionRepository.
type PostgresSubscriptionRepo struct {
	queries *postgres.Queries
	db      *sql.DB
}

// NewPostgresSubscriptionRepo constructs a new PostgresSubscriptionRepo.
func NewPostgresSubscriptionRepo(db *sql.DB) *PostgresSubscriptionRepo {
	return &PostgresSubscriptionRepo{
		queries: postgres.New(db),
		db:      db,
	}
}

// FindByID queries subscriptions by id only.
func (r *PostgresSubscriptionRepo) FindByID(ctx context.Context, id string) (*SubscriptionRow, error) {
	sub, err := r.queries.FindSubscriptionByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return mapFindSubscriptionRow(sub), nil
}

// FindByIDAndTenant queries subscriptions by id and tenant_id in SQL.
func (r *PostgresSubscriptionRepo) FindByIDAndTenant(ctx context.Context, id string, tenantID string) (*SubscriptionRow, error) {
	sub, err := r.queries.FindSubscriptionByIDAndTenant(ctx, postgres.FindSubscriptionByIDAndTenantParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return mapFindSubscriptionByTenantRow(sub), nil
}

// ListByTenant lists subscriptions for a given tenant.
func (r *PostgresSubscriptionRepo) ListByTenant(ctx context.Context, tenantID string) ([]*SubscriptionRow, error) {
	subs, err := r.queries.ListSubscriptionsByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	subscriptions := make([]*SubscriptionRow, len(subs))
	for i, sub := range subs {
		subscriptions[i] = mapListSubscriptionRow(sub)
	}

	return subscriptions, nil
}

// UpdateStatus updates the status for a tenant-scoped subscription record.
func (r *PostgresSubscriptionRepo) UpdateStatus(ctx context.Context, id string, tenantID string, status string) error {
	affected, err := r.queries.UpdateSubscriptionStatus(ctx, postgres.UpdateSubscriptionStatusParams{
		Status:   status,
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresSubscriptionRepo) Update(ctx context.Context, sub *SubscriptionRow, expectedVersion int64) error {
	var nextBilling sql.NullTime
	if sub.NextBilling != "" {
		t, _ := time.Parse(time.RFC3339, sub.NextBilling)
		nextBilling = sql.NullTime{Time: t, Valid: true}
	}

	affected, err := r.queries.UpdateSubscription(ctx, postgres.UpdateSubscriptionParams{
		PlanID:      sub.PlanID,
		Status:      sub.Status,
		Amount:      sub.Amount,
		Currency:    sub.Currency,
		Interval:    sub.Interval,
		NextBilling: nextBilling,
		ID:          sub.ID,
		TenantID:    sub.TenantID,
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

func (r *PostgresSubscriptionRepo) Delete(ctx context.Context, id string, tenantID string, expectedVersion int64) error {
	affected, err := r.queries.DeleteSubscription(ctx, postgres.DeleteSubscriptionParams{
		ID:       id,
		TenantID: tenantID,
		Version:  expectedVersion,
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConcurrentUpdate
	}
	return nil
}

func mapSubscriptionRow(sub interface{}) *SubscriptionRow {
	// Use reflection or type assertion if we generated different types for List and Find
	// Assuming sqlc generated models map similarly
	// We'll extract fields manually to accommodate any generated structs
	// Actually, we can use a generic map or duplicate, but since it's type-safe, we must use the correct type.
	// We'll write specific map functions for the types returned.
	return nil
}

// Map from the generated types:
func mapFindSubscriptionRow(sub postgres.FindSubscriptionByIDRow) *SubscriptionRow {
	var nextBilling string
	if sub.NextBilling.Valid {
		nextBilling = sub.NextBilling.Time.UTC().Format(time.RFC3339)
	}
	var deletedAt *time.Time
	if sub.DeletedAt.Valid {
		t := sub.DeletedAt.Time.UTC()
		deletedAt = &t
	}
	return &SubscriptionRow{
		ID:          sub.ID,
		PlanID:      sub.PlanID,
		TenantID:    sub.TenantID,
		CustomerID:  sub.CustomerID,
		Status:      sub.Status,
		Amount:      sub.Amount,
		Currency:    sub.Currency,
		Interval:    sub.Interval,
		NextBilling: nextBilling,
		UpdatedAt:   sub.UpdatedAt,
		Version:     sub.Version,
		DeletedAt:   deletedAt,
	}
}

func mapFindSubscriptionByTenantRow(sub postgres.FindSubscriptionByIDAndTenantRow) *SubscriptionRow {
	var nextBilling string
	if sub.NextBilling.Valid {
		nextBilling = sub.NextBilling.Time.UTC().Format(time.RFC3339)
	}
	var deletedAt *time.Time
	if sub.DeletedAt.Valid {
		t := sub.DeletedAt.Time.UTC()
		deletedAt = &t
	}
	return &SubscriptionRow{
		ID:          sub.ID,
		PlanID:      sub.PlanID,
		TenantID:    sub.TenantID,
		CustomerID:  sub.CustomerID,
		Status:      sub.Status,
		Amount:      sub.Amount,
		Currency:    sub.Currency,
		Interval:    sub.Interval,
		NextBilling: nextBilling,
		UpdatedAt:   sub.UpdatedAt,
		Version:     sub.Version,
		DeletedAt:   deletedAt,
	}
}

func mapListSubscriptionRow(sub postgres.ListSubscriptionsByTenantRow) *SubscriptionRow {
	var nextBilling string
	if sub.NextBilling.Valid {
		nextBilling = sub.NextBilling.Time.UTC().Format(time.RFC3339)
	}
	var deletedAt *time.Time
	if sub.DeletedAt.Valid {
		t := sub.DeletedAt.Time.UTC()
		deletedAt = &t
	}
	return &SubscriptionRow{
		ID:          sub.ID,
		PlanID:      sub.PlanID,
		TenantID:    sub.TenantID,
		CustomerID:  sub.CustomerID,
		Status:      sub.Status,
		Amount:      sub.Amount,
		Currency:    sub.Currency,
		Interval:    sub.Interval,
		NextBilling: nextBilling,
		UpdatedAt:   sub.UpdatedAt,
		Version:     sub.Version,
		DeletedAt:   deletedAt,
	}
}
