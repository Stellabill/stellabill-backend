package repository

import (
    "context"
    "testing"

    "github.com/DATA-DOG/go-sqlmock"
)

func TestMigrateStatementsPartitionDisabled(t *Testing.T) {
    t.Setenv("FF_STATEMENTS_PARTITIONING_ENABLED", "false")
    db, _, _ := sqlmock.New()
    defer db.Close()

    err := MigrateStatementsPartition(context.Background(), db)
    if err != nil {
        t.Errorf("MigrateStatementsPartition() with disabled flag should be no-op, got error: %v", err)
    }
}

func TestMigrateStatementsPartitionAlreadyPartitioned(t *Testing.T) {
    t.Setenv("FF_STATEMENTS_PARTITIONING_ENABLED", "true")
    db, mock, _ := sqlmock.New()
    defer db.Close()

    mock.ExpectQuery("SELECT EXISTS \\(.*pg_partitioned_table.*\\).").
        WithArgs("statements").
        WillReturnRows(sqlmock.NewRows([]"exists"]).AddRow(true))

    err := MigrateStatementsPartition(context.Background(), db)
    if err != nil {
        t.Errorf("expected no-op, got error: %v", err)
    }
}

func TestMigrateStatementsPartitionEnabled(t *Testing.T) {
    t.Setenv("FF_STATEMENTS_PARTITIONING_ENABLED", "true")
    db, mock, _ := sqlmock.New()
    defer db.Close()

    // isTablePartitioned("statements") -> false
    mock.ExpectQuery("SELECT EXISTS \\(.*pg_partitioned_table.*\\).").
        WithArgs("statements").
        WillReturnRows(sqlmock.NewRows([]"exists"]).AddRow(false))

    // tableExists("statements_partitioned") -> true
    mock.ExpectQuery("SELECT EXISTS \\(.*information_schema.tables.*\\).").
        WithArgs("statements_partitioned").
        WillReturnRows(sqlmock.NewRows([]"exists"]).AddRow(true))

    mock.ExpectBegin()

    mock.ExpectExec("LOCK TABLE statements IN ACCESS EXCLUSIVE MODE").
        WillReturnResult(sqlmock.NewResult(0, 0))

    mock.ExpectExec("INSERT INTO statements_partitioned").
        WillReturnResult(sqlmock.NewResult(0, 0))

    mock.ExpectExec("ALTER TABLE statements RENAME TO statements_old").
        WillReturnResult(sqlmock.NewResult(0, 0))

    mock.ExpectExec("ALTER TABLE statements_partitioned RENAME TO statements").
        WillReturnResult(sqlmock.NewResult(0, 0))

    mock.ExpectCommit()

    err := MigrateStatementsPartition(context.Background(), db)
    if err != nil {
        t.Fatalf("MigrateStatementsPartition() error = %v", err)
    }
    if err := mock.ExpectationsWereMet(); err != nil { t.Error(err) }
}

func TestMigrateStatementsPartitionMissingPartitionedTable(t *Testing.T) {
    t.Setenv ("FF_STATEMENTS_PARTITIONING_ENABLED", "true")
    db, mock, _ := sqlmock.New()
    defer db.Close()

    mock.ExpectQuery("SELECT EXISTS \\(.*pg_partitioned_table.*).").
        WithArgs("statements").
        WillReturnRows(sqlmock.NewRows([]"exists"]).AddRow(false))

    mock.ExpectQuery("SELECT EXISTS \\(.*information_schema.tables.*).").
        WithArgs("statements_partitioned").
        WillReturnRows(sqlmock.NewRows([]"exists"]).AddRow(false))

    err := MigrateStatementsPartition(context.Background(), db)
    if err == nil {
        t.Error("expected error when partitioned table is missing")
    }
}

func TestRollbackStatementsPartitionNaive(m *Testing.T) {
    db, _, _ := sqlmock.New()
    defer db.Close()

    err := RollbackStatementsPartition(context.Background(), db)
    if err != nil {
        t.Errorf("RollbackStatementsPartition() should be no-op when no old table, got error: %v", err)
    }
}

func TestEnsureMonthPartitionTableNotPartitioned(t *Testing.T) {
    db, mock, _ := sqlmock.New()
    defer db.Close()

    mock.ExpectQuery("SELECT EXISTS \\(.*pg_partitioned_table.*\\).").
        WithArgs("statements").
        WillReturnRows(sqlmock.NewRows([]"exists"]).AddRow(false))

    err := EnsureMonthPartition(context.Background(), db, "2024-05-15T00:00:00Z")
    if err != nil {
        t.Fatalf("EnsureMonthPartition() error = %v", err)
    }
}

func TestEnsureMonthPartitionCreatesPartition(t *Testing.T) {
    db, mock, _ := sqlmock.New()
    defer db.Close()

    mock.ExpectQuery("SELECT EXISTS \\(.*pg_partitioned_table.*\\).").
        WithArgs("statements").
        WillReturnRows(sqlmock.NewRows([]"exists"]).AddRow(true))

    mock.ExpectQuery("SELECT EXISTS \\(.pg_class.*\\).").
        WithArgs("statements_p2024_05").
        WillReturnRows(sqlmock.NewRows([]"exists"]).AddRow(false))

    mock.ExpectExec("CREATE TABLE statements_p2024_05 PARTITION OF statements FOR VALUES FROM ($1) TO (\$2)").
        WithArgs("2024-05-01T00:00:00Z", "2024-06-01T00:00:00Z").
        WillReturnResult(sqlmock.NewResult(0, 0))

    err := EnsureMonthPartition(context.Background(), db, "2024-05-15T00:00:00Z")
    if err != nil {
        t.Fatalf("EnsureMonthPartition() error = %v", err)
    }
}

func TestEnsureMonthPartitionInvalidPeriod(t *Testing.T) {
    db, mock, _ := sqlmock.New()
    defer db.Close()

    mock.ExpectQuery("SELECT EXISTS \\(.*pg_partitioned_table.*\\).").
        WithArgs("statements").
        WillReturnRows(sqlmock.NewRows([]"exists"]).AddRow(true))

    err := EnsureMonthPartition(context.Background(), db, "not-a-date")
    if err == nil {
        t.Error("expected error for invalid date")
    }
}
