package migrations

// chaos_drill_test.go validates the properties that the nightly Docker chaos
// drill exercises at the infrastructure level:
//
//  1. A migration whose UpSQL fails rolls back and leaves schema_migrations
//     with zero new rows (no partial record).
//
//  2. Re-running Up after a partial failure skips already-applied migrations
//     and applies only the pending ones (idempotency).
//
//  3. When the DB connection is severed (sql.ErrConnDone) during Up, the
//     runner returns an error and the caller can safely retry.
//
// For the full end-to-end Docker-based drill, see:
//
//	scripts/drills/kill_pg_migration.sh

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestChaosDrill_RollbackOnError verifies that a failed UpSQL does not leave
// a partial row in schema_migrations (matches the "kill before COMMIT" case
// in the Docker drill).
func TestChaosDrill_RollbackOnError(t *testing.T) {
	t.Parallel()
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()

	migs := []Migration{
		{Version: 1, Name: "create_plans", UpSQL: "CREATE TABLE plans (id TEXT PRIMARY KEY);", DownSQL: "DROP TABLE plans;"},
		{Version: 2, Name: "bad_migration", UpSQL: "NOT VALID SQL;", DownSQL: "SELECT 1;"},
	}

	// Version 1 is already applied; version 2 fails.
	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version FROM schema_migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(int64(1)))
	mock.ExpectExec("NOT VALID SQL;").
		WillReturnError(sql.ErrConnDone) // simulate DB kill / exec failure
	mock.ExpectRollback()

	_, err := r.Up(ctx, migs)
	if err == nil {
		t.Fatal("expected error from failed UpSQL, got nil")
	}
	t.Logf("Got expected error: %v", err)

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations not met: %v", err)
	}
}

// TestChaosDrill_IdempotentRecovery verifies that after a partial failure,
// re-running Up applies only the pending migrations (matches the
// "restart + re-migrate" step of the Docker drill).
func TestChaosDrill_IdempotentRecovery(t *testing.T) {
	t.Parallel()
	db, mock := newMockDB(t)
	defer db.Close()

	r := Runner{DB: db}
	ctx := context.Background()

	migs := []Migration{
		{Version: 1, Name: "create_plans", UpSQL: "CREATE TABLE plans (id TEXT PRIMARY KEY);", DownSQL: "DROP TABLE plans;"},
		{Version: 2, Name: "create_subscriptions", UpSQL: "CREATE TABLE subscriptions (id TEXT PRIMARY KEY);", DownSQL: "DROP TABLE subscriptions;"},
	}

	// Recovery run: version 1 is already applied; version 2 must be applied.
	mock.ExpectBegin()
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("LOCK TABLE schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT version FROM schema_migrations").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(int64(1)))
	mock.ExpectExec("CREATE TABLE subscriptions").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO schema_migrations").
		WithArgs(int64(2), "create_subscriptions").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	applied, err := r.Up(ctx, migs)
	if err != nil {
		t.Fatalf("Up recovery: unexpected error: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("expected 1 newly applied migration, got %d", len(applied))
	}
	if applied[0].Version != 2 {
		t.Errorf("expected version 2 to be applied, got %d", applied[0].Version)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations not met: %v", err)
	}
}

// TestChaosDrill_ConnectionDropDuringBegin verifies that a connection failure
// at transaction start surfaces as an error and does not panic — modelling
// what happens when Postgres is killed before the migration even begins.
func TestChaosDrill_ConnectionDropDuringBegin(t *testing.T) {
	t.Parallel()
	// Close the DB before calling Up so BeginTx returns an error.
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	_ = db.Close()

	r := Runner{DB: db}
	mig := Migration{Version: 1, Name: "create_plans", UpSQL: "CREATE TABLE plans (id TEXT PRIMARY KEY);", DownSQL: "DROP TABLE plans;"}

	_, err = r.Up(context.Background(), []Migration{mig})
	if err == nil {
		t.Fatal("expected error when DB connection is dropped at BeginTx, got nil")
	}
	t.Logf("Got expected error: %v", err)
}
