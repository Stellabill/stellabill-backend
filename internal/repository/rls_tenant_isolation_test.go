package repository

import (
	"context"
	"database/sql"
	"regexp"
	"stellarbill-backend/internal/db"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresPlanRepo_TenantIsolation_WrongTenantReturnsZeroRows(t *testing.T) {
	dbMock, mock := newPlanSQLMock(t)

	ctx := db.ContextWithTenantID(context.Background(), "tenant-A")
	repo := NewPostgresPlanRepo(dbMock)

	tenantA := "tenant-A"
	tenantB := "tenant-B"

	mock.ExpectQuery(regexp.QuoteMeta(listPlansQuery)).
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "tenant_id", "name", "amount", "currency", "interval", "description", "updated_at", "version"},
		).
			AddRow("plan-1", tenantA, "Plan A", "999", "USD", "month", "Desc A", mustParseTime("2024-01-01T00:00:00Z"), 1).
			AddRow("plan-2", tenantA, "Plan B", "1999", "USD", "year", "Desc B", mustParseTime("2024-01-01T00:00:00Z"), 1))

	plansTenantA, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, plansTenantA, 2, "tenant-A should see its own 2 plans")
	for _, p := range plansTenantA {
		assert.Equal(t, tenantA, p.TenantID)
	}

	ctxB := db.ContextWithTenantID(context.Background(), tenantB)

	mock.ExpectQuery(regexp.QuoteMeta(listPlansQuery)).
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "tenant_id", "name", "amount", "currency", "interval", "description", "updated_at", "version"},
		))

	plansTenantB, err := repo.List(ctxB)
	require.NoError(t, err)
	assert.Len(t, plansTenantB, 0,
		"negative test: tenant-B with RLS active must see ZERO rows for tenant-A data, not leak cross-tenant")

	assertSQLExpectations(t, mock)
}

func TestPostgresPlanRepo_FindByID_WrongTenantReturnsNotFound(t *testing.T) {
	dbMock, mock := newPlanSQLMock(t)

	ctxA := db.ContextWithTenantID(context.Background(), "tenant-A")
	ctxB := db.ContextWithTenantID(context.Background(), "tenant-B")
	repo := NewPostgresPlanRepo(dbMock)

	mock.ExpectQuery(regexp.QuoteMeta(findPlanByIDQuery)).
		WithArgs("plan-owned-by-A").
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "tenant_id", "name", "amount", "currency", "interval", "description", "updated_at", "version"},
		).AddRow("plan-owned-by-A", "tenant-A", "Plan A", "999", "USD", "month", "Owned", mustParseTime("2024-01-01T00:00:00Z"), 1))

	got, err := repo.FindByID(ctxA, "plan-owned-by-A")
	require.NoError(t, err)
	require.Equal(t, "tenant-A", got.TenantID)

	mock.ExpectQuery(regexp.QuoteMeta(findPlanByIDQuery)).
		WithArgs("plan-owned-by-A").
		WillReturnError(sql.ErrNoRows)

	_, err = repo.FindByID(ctxB, "plan-owned-by-A")
	require.ErrorIs(t, err, ErrNotFound,
		"negative test: querying tenant-A's plan with tenant-B context must return ErrNotFound via RLS, not return the row")

	assertSQLExpectations(t, mock)
}

func TestPostgresPlanRepo_MissingTenantContextFailsClosed(t *testing.T) {
	rawDB, _ := newPlanSQLMock(t)
	rlsDB := db.NewRLSDB(rawDB)

	ctx := context.Background()

	_, err := rlsDB.QueryContext(ctx, "SELECT id FROM plans WHERE id = $1", "any-plan-id")
	require.Error(t, err,
		"missing tenant context must fail closed, not proceed without RLS")
	assert.ErrorIs(t, err, db.ErrMissingTenantContext,
		"error must be the explicit ErrMissingTenantContext sentinel")

	_, err = rlsDB.QueryContext(ctx, "SELECT id, tenant_id, name, amount FROM plans ORDER BY name, id")
	require.Error(t, err,
		"List without tenant context must fail closed, not return all rows")
	assert.ErrorIs(t, err, db.ErrMissingTenantContext)
}

func TestPostgresSubscriptionRepo_MissingTenantContextFailsClosed(t *testing.T) {
	rawDB, _ := newPlanSQLMock(t)
	rlsDB := db.NewRLSDB(rawDB)

	ctx := context.Background()

	_, err := rlsDB.QueryContext(ctx, "SELECT id FROM subscriptions WHERE id = $1", "sub-1")
	require.Error(t, err)
	assert.ErrorIs(t, err, db.ErrMissingTenantContext)

	_, err = rlsDB.ExecContext(ctx, "UPDATE subscriptions SET status = 'paused' WHERE id = $1", "sub-1")
	require.Error(t, err)
	assert.ErrorIs(t, err, db.ErrMissingTenantContext)
}

func TestTenantIsolation_PlanRowIncludesTenantID(t *testing.T) {
	dbMock, mock := newPlanSQLMock(t)
	ctx := db.ContextWithTenantID(context.Background(), "tenant-gamma")
	repo := NewPostgresPlanRepo(dbMock)

	expected := &PlanRow{
		ID:          "plan-x",
		TenantID:    "tenant-gamma",
		Name:        "Gamma Plan",
		Amount:      "2500",
		Currency:    "EUR",
		Interval:    "month",
		Description: "Paid tier",
		Version:     3,
	}

	mock.ExpectQuery(regexp.QuoteMeta(findPlanByIDQuery)).
		WithArgs("plan-x").
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "tenant_id", "name", "amount", "currency", "interval", "description", "updated_at", "version"},
		).AddRow(
			expected.ID, expected.TenantID, expected.Name, expected.Amount,
			expected.Currency, expected.Interval, expected.Description,
			mustParseTime("2024-03-15T12:00:00Z"), expected.Version,
		))

	got, err := repo.FindByID(ctx, "plan-x")
	require.NoError(t, err)
	assert.Equal(t, expected.ID, got.ID)
	assert.Equal(t, expected.TenantID, got.TenantID,
		"every PlanRow returned from the repository must carry the tenant_id field for defense-in-depth application checks")
	assert.Equal(t, expected.Version, got.Version)

	assertSQLExpectations(t, mock)
}

func TestRLSDB_ExecContext_MissingTenantFailsClosed(t *testing.T) {
	rawDB, _ := newPlanSQLMock(t)
	rlsDB := db.NewRLSDB(rawDB)

	_, err := rlsDB.ExecContext(context.Background(), "SELECT 1")
	require.Error(t, err)
	assert.ErrorIs(t, err, db.ErrMissingTenantContext,
		"direct ExecContext on RLSDB without tenant must fail closed")
}

func TestRLSDB_BeginTx_MissingTenantFailsClosed(t *testing.T) {
	rawDB, _ := newPlanSQLMock(t)
	rlsDB := db.NewRLSDB(rawDB)

	_, err := rlsDB.BeginTx(context.Background(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, db.ErrMissingTenantContext,
		"BeginTx on RLSDB without tenant context must fail closed, not begin a transaction with no RLS scope")
}

func mustParseTime(s string) interface{} {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
