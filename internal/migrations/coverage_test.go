package migrations

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLoadDir_VersionTooLarge(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "999999999999999999999999999999_big.up.sql"), "SELECT 1;")
	writeFile(t, filepath.Join(dir, "999999999999999999999999999999_big.down.sql"), "SELECT -1;")
	if _, err := LoadDir(dir); err == nil {
		t.Fatalf("expected error")
	}
}

func TestReadSQLFile_IsDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := readSQLFile(dir); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Applied_QueryError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version, name, applied_at FROM schema_migrations").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Applied(ctx); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Applied_EnsureSchemaError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Applied(ctx); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Applied_LockError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Applied(ctx); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Applied_ScanError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version, name, applied_at FROM schema_migrations").WillReturnRows(
		sqlmock.NewRows([]string{"version", "name", "applied_at"}).
			AddRow(int64(1), "init", "not-a-time"),
	)
	mock.ExpectRollback()

	if _, err := r.Applied(ctx); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Up_AppliedVersionsQueryError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version FROM schema_migrations").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Up(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Up_RecordInsertError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version FROM schema_migrations").WillReturnRows(sqlmock.NewRows([]string{"version"}))
	mock.ExpectExec("SELECT 1;").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations").WithArgs(int64(1), "init").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Up(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Up_EnsureSchemaError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Up(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Up_LockError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Up(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Up_CommitError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version FROM schema_migrations").WillReturnRows(sqlmock.NewRows([]string{"version"}))
	mock.ExpectExec("SELECT 1;").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations").WithArgs(int64(1), "init").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Up(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Down_DownExecError(t *testing.T) {
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
	mock.ExpectExec("SELECT -1;").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Down(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Down_LockError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Down(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Down_EnsureSchemaError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Down(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Down_QueryRowError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version, name FROM schema_migrations").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Down(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Down_NoRows_CommitError(t *testing.T) {
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
	mock.ExpectCommit().WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Down(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Down_DeleteError(t *testing.T) {
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
	mock.ExpectExec("DELETE FROM schema_migrations").WithArgs(int64(1)).WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Down(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Down_CommitError(t *testing.T) {
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
	mock.ExpectCommit().WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Down(ctx, migs); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_AppliedVersions_ScanError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	ctx := context.Background()
	txDB := Runner{DB: db}
	migs := []Migration{{Version: 1, Name: "init", UpSQL: "SELECT 1;", DownSQL: "SELECT -1;"}}

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version FROM schema_migrations").WillReturnRows(
		sqlmock.NewRows([]string{"version"}).AddRow("bad"),
	)
	mock.ExpectRollback()

	_, err := txDB.Up(ctx, migs)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunner_Applied_CommitError(t *testing.T) {
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version, name, applied_at FROM schema_migrations").WillReturnRows(
		sqlmock.NewRows([]string{"version", "name", "applied_at"}).
			AddRow(int64(1), "init", time.Now().UTC()),
	)
	mock.ExpectCommit().WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if _, err := r.Applied(ctx); err == nil {
		t.Fatalf("expected error")
	}
}
