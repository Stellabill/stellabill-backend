package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"stellarbill-backend/internal/repository/postgres"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var subRepoTracer = otel.Tracer("repository/subscriptions")

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
	ctx, span := subRepoTracer.Start(ctx, "SubscriptionRepo.FindByID", trace.WithAttributes(
		attribute.String("subscription.id", id),
	))
	defer span.End()

	sub, err := r.queries.FindSubscriptionByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.SetStatus(codes.Error, "subscription not found")
			return nil, ErrNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	return mapFindSubscriptionRow(sub), nil
}

// FindByIDAndTenant queries subscriptions by id and tenant_id in SQL.
func (r *PostgresSubscriptionRepo) FindByIDAndTenant(ctx context.Context, id string, tenantID string) (*SubscriptionRow, error) {
	ctx, span := subRepoTracer.Start(ctx, "SubscriptionRepo.FindByID", trace.WithAttributes(
		attribute.String("subscription.id", id),
		attribute.String("tenant.id", tenantID),
	))
	defer span.End()

	sub, err := r.queries.FindSubscriptionByIDAndTenant(ctx, postgres.FindSubscriptionByIDAndTenantParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			span.SetStatus(codes.Error, "subscription not found")
			return nil, ErrNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	return mapFindSubscriptionByTenantRow(sub), nil
}

// FindByIDsAndTenant queries subscriptions by multiple IDs and optional tenant_id in SQL.
func (r *PostgresSubscriptionRepo) FindByIDsAndTenant(ctx context.Context, ids []string, tenantID string) ([]*SubscriptionRow, error) {
	if len(ids) == 0 {
		return []*SubscriptionRow{}, nil
	}
	if r.db == nil {
		return []*SubscriptionRow{}, nil
	}
	args := make([]interface{}, 0, len(ids)+1)
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, id)
	}
	q := "SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval, next_billing, updated_at, version, deleted_at FROM subscriptions WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	if tenantID != "" {
		args = append(args, tenantID)
		q += fmt.Sprintf(" AND tenant_id = $%d", len(args))
	}

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*SubscriptionRow
	for rows.Next() {
		var s SubscriptionRow
		var nextBilling sql.NullTime
		var deletedAt sql.NullTime
		if err := rows.Scan(
			&s.ID, &s.PlanID, &s.TenantID, &s.CustomerID, &s.Status,
			&s.Amount, &s.Currency, &s.Interval, &nextBilling,
			&s.UpdatedAt, &s.Version, &deletedAt,
		); err != nil {
			return nil, err
		}
		if nextBilling.Valid {
			s.NextBilling = nextBilling.Time.UTC().Format(time.RFC3339)
		}
		if deletedAt.Valid {
			t := deletedAt.Time.UTC()
			s.DeletedAt = &t
		}
		result = append(result, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
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
