package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"stellarbill-backend/internal/config"
)

func TestPostgresPlanRepoFindByID(t *testing.T) {
	db, mock := newPlanSQLMock(t)
	repo := NewPostgresPlanRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(findPlanByIDQuery)).
		WithArgs("plan_basic").
		WillReturnRows(sqlmock.NewRows(planColumns()).
			AddRow("plan_basic", "Basic", "999", "USD", "month", "Starter plan"))

	got, err := repo.FindByID(context.Background(), "plan_basic")
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}

	if got.ID != "plan_basic" ||
		got.Name != "Basic" ||
		got.Amount != "999" ||
		got.Currency != "USD" ||
		got.Interval != "month" ||
		got.Description != "Starter plan" {
		t.Fatalf("unexpected plan row: %#v", got)
	}

	assertSQLExpectations(t, mock)
}

func TestPostgresPlanRepoFindByIDNotFound(t *testing.T) {
	db, mock := newPlanSQLMock(t)
	repo := NewPostgresPlanRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(findPlanByIDQuery)).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindByID(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	assertSQLExpectations(t, mock)
}

func TestPostgresPlanRepoFindByIDQueryError(t *testing.T) {
	db, mock := newPlanSQLMock(t)
	repo := NewPostgresPlanRepo(db)
	wantErr := errors.New("database unavailable")

	mock.ExpectQuery(regexp.QuoteMeta(findPlanByIDQuery)).
		WithArgs("plan_basic").
		WillReturnError(wantErr)

	_, err := repo.FindByID(context.Background(), "plan_basic")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected query error, got %v", err)
	}

	assertSQLExpectations(t, mock)
}

func TestPostgresPlanRepoList(t *testing.T) {
	db, mock := newPlanSQLMock(t)
	repo := NewPostgresPlanRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(listPlansQuery)).
		WillReturnRows(sqlmock.NewRows(planColumns()).
			AddRow("plan_basic", "Basic", "999", "USD", "month", nil).
			AddRow("plan_pro", "Pro", "2999", "USD", "month", "For growing teams"))

	got, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(got))
	}
	if got[0].Description != "" {
		t.Fatalf("expected NULL description to map to empty string, got %q", got[0].Description)
	}
	if got[1].Description != "For growing teams" {
		t.Fatalf("unexpected description: %q", got[1].Description)
	}

	assertSQLExpectations(t, mock)
}

func TestPostgresPlanRepoListEmpty(t *testing.T) {
	db, mock := newPlanSQLMock(t)
	repo := NewPostgresPlanRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(listPlansQuery)).
		WillReturnRows(sqlmock.NewRows(planColumns()))

	got, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %d plans", len(got))
	}

	assertSQLExpectations(t, mock)
}

func TestPostgresPlanRepoListQueryError(t *testing.T) {
	db, mock := newPlanSQLMock(t)
	repo := NewPostgresPlanRepo(db)
	wantErr := errors.New("query failed")

	mock.ExpectQuery(regexp.QuoteMeta(listPlansQuery)).
		WillReturnError(wantErr)

	_, err := repo.List(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected query error, got %v", err)
	}

	assertSQLExpectations(t, mock)
}

func TestPostgresPlanRepoListRowsError(t *testing.T) {
	db, mock := newPlanSQLMock(t)
	repo := NewPostgresPlanRepo(db)
	wantErr := errors.New("row iteration failed")

	rows := sqlmock.NewRows(planColumns()).
		AddRow("plan_basic", "Basic", "999", "USD", "month", nil).
		RowError(0, wantErr)
	mock.ExpectQuery(regexp.QuoteMeta(listPlansQuery)).WillReturnRows(rows)

	_, err := repo.List(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected rows error, got %v", err)
	}

	assertSQLExpectations(t, mock)
}

func TestPostgresPlanRepoListScanError(t *testing.T) {
	db, mock := newPlanSQLMock(t)
	repo := NewPostgresPlanRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(listPlansQuery)).
		WillReturnRows(sqlmock.NewRows(planColumns()).
			AddRow("plan_basic", nil, "999", "USD", "month", nil))

	if _, err := repo.List(context.Background()); err == nil {
		t.Fatal("expected scan error")
	}

	assertSQLExpectations(t, mock)
}

func TestApplySQLDBPoolConfig(t *testing.T) {
	db, mock := newPlanSQLMock(t)

	cfg := config.Config{
		DBPoolMaxConns:        7,
		DBPoolMinConns:        3,
		DBPoolMaxConnLifetime: 120,
		DBPoolMaxConnIdleTime: 30,
	}
	ApplySQLDBPoolConfig(db, cfg)

	stats := db.Stats()
	if stats.MaxOpenConnections != 7 {
		t.Fatalf("expected max open connections 7, got %d", stats.MaxOpenConnections)
	}

	ctx := context.Background()
	mock.ExpectPing()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext after pool config returned error: %v", err)
	}

	assertSQLExpectations(t, mock)
}

func TestApplySQLDBPoolConfigNilDB(t *testing.T) {
	ApplySQLDBPoolConfig(nil, config.Config{
		DBPoolMaxConns:        1,
		DBPoolMinConns:        1,
		DBPoolMaxConnLifetime: 1,
		DBPoolMaxConnIdleTime: 1,
	})
}

func newPlanSQLMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db, mock
}

func planColumns() []string {
	return []string{"id", "name", "amount", "currency", "interval", "description"}
}

func assertSQLExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}
