package migrations

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRunner_Up_Idempotent(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{
		{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"},
		{Version: 2, Name: "second", UpSQL: "SELECT 2;", DownSQL: "SELECT -2;"},
	}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version FROM schema_migrations").WillReturnRows(
		sqlmock.NewRows([]string{"version"}).AddRow(int64(1)),
	)
	mock.ExpectExec("SELECT 2;").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations").WithArgs(int64(2), "second").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	applied, err := r.Up(ctx, migs)
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if len(applied) != 1 || applied[0].Version != 2 {
		t.Fatalf("unexpected applied: %#v", applied)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestRunner_Validate_NilDB(t *testing.T) {
	if err := (Runner{}).Validate(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Applied_BeginTxError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	_ = db.Close()

	if _, err := (Runner{DB: db}).Applied(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Up_BeginTxError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	_ = db.Close()

	_, err = (Runner{DB: db}).Up(context.Background(), []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Down_BeginTxError(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	_ = db.Close()

	_, err = (Runner{DB: db}).Down(context.Background(), []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Up_NoMigrations(t *testing.T) {
	db, _ := newMockDB(t)
	defer db.Close()

	_, err := (Runner{DB: db}).Up(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Down_NoMigrations(t *testing.T) {
	db, _ := newMockDB(t)
	defer db.Close()

	_, err := (Runner{DB: db}).Down(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Up_RollsBackOnFailure(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "BAD SQL", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version FROM schema_migrations").WillReturnRows(sqlmock.NewRows([]string{"version"}))
	mock.ExpectExec("BAD SQL").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	_, err := r.Up(ctx, migs)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestRunner_Down_NoRows_NoOp(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version, name FROM schema_migrations").WillReturnRows(
		sqlmock.NewRows([]string{"version", "name"}),
	)
	mock.ExpectCommit()

	m, err := r.Down(ctx, migs)
	if err != nil {
		t.Fatalf("Down: %v", err)
	}
	if m != nil {
		t.Fatalf("expected nil migration, got %#v", m)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestRunner_Down_AppliesAndDeletes(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version, name FROM schema_migrations").WillReturnRows(
		sqlmock.NewRows([]string{"version", "name"}).AddRow(int64(1), "init"),
	)
	mock.ExpectExec("SELECT -1;").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM schema_migrations").WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	m, err := r.Down(ctx, migs)
	if err != nil {
		t.Fatalf("Down: %v", err)
	}
	if m == nil || m.Version != 1 {
		t.Fatalf("unexpected migration: %#v", m)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestRunner_Down_MissingLocalMigration(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version, name FROM schema_migrations").WillReturnRows(
		sqlmock.NewRows([]string{"version", "name"}).AddRow(int64(2), "second"),
	)
	mock.ExpectRollback()

	_, err := r.Down(ctx, migs)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestRunner_Applied_ReturnsRows(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version, name, applied_at FROM schema_migrations").WillReturnRows(
		sqlmock.NewRows([]string{"version", "name", "applied_at"}).
			AddRow(int64(1), "init", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	)
	mock.ExpectCommit()

	_, err := r.Applied(ctx)
	if err != nil {
		t.Fatalf("Applied: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	return db, mock
}

