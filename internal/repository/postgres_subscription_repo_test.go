package repository

import (
    "context"
    "regexp"
    "testing"
    "time"

    "github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresSubscriptionRepo_FindByID_HappyPath(t *testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil {
        t.Fatalf("failed to create sqlmock: %v", err)
    }
    defer db.Close()

    repo := NewPostgresSubscriptionRepo(db)
    id := "sub-1"
    tenantID := "tenant-1"
    customerID := "cust-1"
    nextBilling := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
    deletedAt := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

    rows := sqlmock.NewRows([]string{
        "id", "plan_id", "tenant_id", "customer_id", "status",
        "amount", "currency", "interval", "next_billing", "deleted_at",
    }).AddRow(
        id,
        "plan-a",
        tenantID,
        customerID,
        "active",
        "1999",
        "usd",
        "monthly",
        nextBilling,
        deletedAt,
    )

    query := "SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval, next_billing, deleted_at FROM subscriptions WHERE id = $1"
    mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(id).WillReturnRows(rows)

    got, err := repo.FindByID(context.Background(), id)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if got.ID != id || got.PlanID != "plan-a" || got.TenantID != tenantID || got.CustomerID != customerID {
        t.Fatalf("unexpected row: %+v", got)
    }
    if got.NextBilling != nextBilling.Format(time.RFC3339) {
        t.Fatalf("expected next billing %q, got %q", nextBilling.Format(time.RFC3339), got.NextBilling)
    }
    if got.DeletedAt == nil || !got.DeletedAt.Equal(deletedAt) {
        t.Fatalf("expected deleted at %v, got %v", deletedAt, got.DeletedAt)
    }
    if err := mock.ExpectationsWereMet(); err != nil {
        t.Fatalf("unmet expectations: %v", err)
    }
}

func TestPostgresSubscriptionRepo_FindByIDAndTenant_HappyPath(t *testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil {
        t.Fatalf("failed to create sqlmock: %v", err)
    }
    defer db.Close()

    repo := NewPostgresSubscriptionRepo(db)
    id := "sub-2"
    tenantID := "tenant-2"
    nextBilling := time.Date(2026, 7, 1, 9, 30, 0, 0, time.UTC)

    rows := sqlmock.NewRows([]string{
        "id", "plan_id", "tenant_id", "customer_id", "status",
        "amount", "currency", "interval", "next_billing", "deleted_at",
    }).AddRow(
        id,
        "plan-b",
        tenantID,
        "cust-2",
        "past_due",
        "2999",
        "eur",
        "yearly",
        nextBilling,
        nil,
    )

    query := "SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval, next_billing, deleted_at FROM subscriptions WHERE id = $1 AND tenant_id = $2"
    mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(id, tenantID).WillReturnRows(rows)

    got, err := repo.FindByIDAndTenant(context.Background(), id, tenantID)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if got.TenantID != tenantID || got.ID != id || got.DeletedAt != nil {
        t.Fatalf("unexpected row mismatch: %+v", got)
    }
    if got.NextBilling != nextBilling.Format(time.RFC3339) {
        t.Fatalf("expected next billing string, got %q", got.NextBilling)
    }
    if err := mock.ExpectationsWereMet(); err != nil {
        t.Fatalf("unmet expectations: %v", err)
    }
}

func TestPostgresSubscriptionRepo_FindByIDAndTenant_CrossTenantReturnsNotFound(t *testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil {
        t.Fatalf("failed to create sqlmock: %v", err)
    }
    defer db.Close()

    repo := NewPostgresSubscriptionRepo(db)
    id := "sub-3"
    tenantID := "tenant-3"

    rows := sqlmock.NewRows([]string{
        "id", "plan_id", "tenant_id", "customer_id", "status",
        "amount", "currency", "interval", "next_billing", "deleted_at",
    })

    query := "SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval, next_billing, deleted_at FROM subscriptions WHERE id = $1 AND tenant_id = $2"
    mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(id, tenantID).WillReturnRows(rows)

    _, err = repo.FindByIDAndTenant(context.Background(), id, tenantID)
    if err != ErrNotFound {
        t.Fatalf("expected ErrNotFound, got %v", err)
    }
    if err := mock.ExpectationsWereMet(); err != nil {
        t.Fatalf("unmet expectations: %v", err)
    }
}

func TestPostgresSubscriptionRepo_FindByID_NullNextBillingAndNoDeletedAt(t *testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil {
        t.Fatalf("failed to create sqlmock: %v", err)
    }
    defer db.Close()

    repo := NewPostgresSubscriptionRepo(db)
    id := "sub-4"

    rows := sqlmock.NewRows([]string{
        "id", "plan_id", "tenant_id", "customer_id", "status",
        "amount", "currency", "interval", "next_billing", "deleted_at",
    }).AddRow(
        id,
        "plan-c",
        "tenant-4",
        "cust-4",
        "canceled",
        "3999",
        "gbp",
        "monthly",
        nil,
        nil,
    )

    query := "SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval, next_billing, deleted_at FROM subscriptions WHERE id = $1"
    mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(id).WillReturnRows(rows)

    got, err := repo.FindByID(context.Background(), id)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if got.NextBilling != "" {
        t.Fatalf("expected empty next billing, got %q", got.NextBilling)
    }
    if got.DeletedAt != nil {
        t.Fatalf("expected nil DeletedAt, got %v", got.DeletedAt)
    }
    if err := mock.ExpectationsWereMet(); err != nil {
        t.Fatalf("unmet expectations: %v", err)
    }
}

func TestPostgresSubscriptionRepo_FindByID_NoRowsReturnsNotFound(t *testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil {
        t.Fatalf("failed to create sqlmock: %v", err)
    }
    defer db.Close()

    repo := NewPostgresSubscriptionRepo(db)
    id := "sub-5"

    rows := sqlmock.NewRows([]string{
        "id", "plan_id", "tenant_id", "customer_id", "status",
        "amount", "currency", "interval", "next_billing", "deleted_at",
    })

    query := "SELECT id, plan_id, tenant_id, customer_id, status, amount, currency, interval, next_billing, deleted_at FROM subscriptions WHERE id = $1"
    mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(id).WillReturnRows(rows)

    _, err = repo.FindByID(context.Background(), id)
    if err != ErrNotFound {
        t.Fatalf("expected ErrNotFound, got %v", err)
    }
    if err := mock.ExpectationsWereMet(); err != nil {
        t.Fatalf("unmet expectations: %v", err)
    }
}

func TestPostgresSubscriptionRepo_UpdateStatus_HappyPath(t *testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil {
        t.Fatalf("failed to create sqlmock: %v", err)
    }
    defer db.Close()

    repo := NewPostgresSubscriptionRepo(db)
    id := "sub-6"
    tenantID := "tenant-6"
    status := "active"

    mock.ExpectExec(regexp.QuoteMeta(`
        UPDATE subscriptions
        SET status = $1
        WHERE id = $2 AND tenant_id = $3
    `)).WithArgs(status, id, tenantID).WillReturnResult(sqlmock.NewResult(0, 1))

    err = repo.UpdateStatus(context.Background(), id, tenantID, status)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if err := mock.ExpectationsWereMet(); err != nil {
        t.Fatalf("unmet expectations: %v", err)
    }
}

func TestPostgresSubscriptionRepo_UpdateStatus_NotFound(t *testing.T) {
    db, mock, err := sqlmock.New()
    if err != nil {
        t.Fatalf("failed to create sqlmock: %v", err)
    }
    defer db.Close()

    repo := NewPostgresSubscriptionRepo(db)
    id := "sub-7"
    tenantID := "tenant-7"
    status := "inactive"

    mock.ExpectExec(regexp.QuoteMeta(`
        UPDATE subscriptions
        SET status = $1
        WHERE id = $2 AND tenant_id = $3
    `)).WithArgs(status, id, tenantID).WillReturnResult(sqlmock.NewResult(0, 0))

    err = repo.UpdateStatus(context.Background(), id, tenantID, status)
    if err != ErrNotFound {
        t.Fatalf("expected ErrNotFound, got %v", err)
    }
    if err := mock.ExpectationsWereMet(); err != nil {
        t.Fatalf("unmet expectations: %v", err)
    }
}
