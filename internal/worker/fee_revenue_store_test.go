package worker

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func newStoreMock(t *testing.T) (*sqlFeeRevenueStore, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	return &sqlFeeRevenueStore{db: db}, mock, func() { _ = db.Close() }
}

func TestSQLStore_Refresh_NonConcurrent(t *testing.T) {
	store, mock, done := newStoreMock(t)
	defer done()

	// Non-concurrent refresh must NOT contain CONCURRENTLY.
	mock.ExpectExec("^REFRESH MATERIALIZED VIEW mv_fee_revenue_monthly$").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := store.Refresh(context.Background(), false); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSQLStore_Refresh_Concurrent(t *testing.T) {
	store, mock, done := newStoreMock(t)
	defer done()

	// Concurrent refresh MUST issue CONCURRENTLY so readers are not blocked.
	mock.ExpectExec("REFRESH MATERIALIZED VIEW CONCURRENTLY mv_fee_revenue_monthly").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := store.Refresh(context.Background(), true); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSQLStore_Refresh_Error(t *testing.T) {
	store, mock, done := newStoreMock(t)
	defer done()

	mock.ExpectExec("REFRESH MATERIALIZED VIEW").WillReturnError(errors.New("boom"))

	if err := store.Refresh(context.Background(), false); err == nil {
		t.Fatal("expected error")
	}
}

func TestSQLStore_MarkRefreshed(t *testing.T) {
	store, mock, done := newStoreMock(t)
	defer done()

	at := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec("UPDATE mv_fee_revenue_refresh_state SET last_refreshed_at").
		WithArgs(at, true).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.MarkRefreshed(context.Background(), at); err != nil {
		t.Fatalf("MarkRefreshed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestSQLStore_LastRefreshedAt_Set(t *testing.T) {
	store, mock, done := newStoreMock(t)
	defer done()

	at := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT last_refreshed_at FROM mv_fee_revenue_refresh_state").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{"last_refreshed_at"}).AddRow(at))

	got, ok, err := store.LastRefreshedAt(context.Background())
	if err != nil {
		t.Fatalf("LastRefreshedAt: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !got.Equal(at) {
		t.Errorf("got %v, want %v", got, at)
	}
}

func TestSQLStore_LastRefreshedAt_NullNeverRefreshed(t *testing.T) {
	store, mock, done := newStoreMock(t)
	defer done()

	// last_refreshed_at IS NULL -> never refreshed.
	mock.ExpectQuery("SELECT last_refreshed_at FROM mv_fee_revenue_refresh_state").
		WithArgs(true).
		WillReturnRows(sqlmock.NewRows([]string{"last_refreshed_at"}).AddRow(nil))

	_, ok, err := store.LastRefreshedAt(context.Background())
	if err != nil {
		t.Fatalf("LastRefreshedAt: %v", err)
	}
	if ok {
		t.Error("expected ok=false for NULL last_refreshed_at")
	}
}

func TestSQLStore_LastRefreshedAt_NoRows(t *testing.T) {
	store, mock, done := newStoreMock(t)
	defer done()

	// Missing singleton row -> treated as never refreshed, not an error.
	mock.ExpectQuery("SELECT last_refreshed_at FROM mv_fee_revenue_refresh_state").
		WithArgs(true).
		WillReturnError(sql.ErrNoRows)

	_, ok, err := store.LastRefreshedAt(context.Background())
	if err != nil {
		t.Fatalf("LastRefreshedAt: %v", err)
	}
	if ok {
		t.Error("expected ok=false when row is missing")
	}
}

func TestSQLStore_LastRefreshedAt_QueryError(t *testing.T) {
	store, mock, done := newStoreMock(t)
	defer done()

	mock.ExpectQuery("SELECT last_refreshed_at FROM mv_fee_revenue_refresh_state").
		WithArgs(true).
		WillReturnError(errors.New("db down"))

	if _, _, err := store.LastRefreshedAt(context.Background()); err == nil {
		t.Error("expected query error to propagate")
	}
}

func TestNewFeeRevenueRefreshJob_Constructor(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	j := NewFeeRevenueRefreshJob(db, FeeRevenueRefreshConfig{}, nil)
	if j == nil {
		t.Fatal("expected non-nil job")
	}
	if _, ok := j.store.(*sqlFeeRevenueStore); !ok {
		t.Errorf("expected sqlFeeRevenueStore, got %T", j.store)
	}
	// Defaults must be applied.
	if j.config.PollInterval != time.Hour {
		t.Errorf("PollInterval = %v, want 1h", j.config.PollInterval)
	}
}
