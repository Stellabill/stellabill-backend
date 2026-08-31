package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"
)

const statementsPartitionEnv = "FF_STATEMENTS_PARTITIONING_ENABLED"

// MigrateStatementsPartition creates the partitioned statements table and migrates data.
// This is a one-time migration that should be run with a maintenance window.
func MigrateStatementsPartition(ctx context.Context, db *sql.DB) error {
	if !statementsPartitioningEnabled() {
		return nil // feature flag off; no-op
	}

	// Check if the target partitioned table already exists.
	exists, err := tableExists(ctx, db, "statements_partitioned")
	if err != nil {
		return err
	}
	if exists {
		return nil // already migrated
	}

	exists, err = tableExists(ctx, db, "statements_partitioned")
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("statements_partitioned table does not exist; run migrations first")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback()

	// Block writes to the old table while we copy and rename.
	if _, err := tx.ExecContext(ctx, "LOCK TABLE statements IN ACCESS EXCLUSIVE MODE"); err != nil {
		return fmt.Errorf("lock statements: %w", err)
	}

	// Copy data from old table to new partitioned table.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO statements_partitioned (
			id, tenant_id, content, created_at, updated_at
		)
		SELECT id, tenant_id, content, created_at, updated_at
		FROM statements
	`); err != nil {
		return fmt.Errorf("copy data to partitioned table: %w", err)
	}

	// Swap table names.
	if _, err := tx.ExecContext(ctx, "ALTER TABLE statements RENAME TO statements_old"); err != nil {
		return fmt.Errorf("rename statements to statements_old: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "ALTER TABLE statements_partitioned RENAME TO statements"); err != nil {
		return fmt.Errorf("rename statements_partitioned to statements: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration transaction: %w", err)
	}
	return nil
}

// RollbackStatementsPartition reverts the migration by renaming tables back.
func RollbackStatementsPartition(ctx context.Context, db *sql.DB) error {
	if !statementsPartitioningEnabled() {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rollback transaction: %w", err)
	}
	defer tx.Rollback()

	// Block writes.
	if _, err := tx.ExecContext(ctx, "LOCK TABLE statements IN ACCESS EXCLUSIVE MODE"); err != nil {
		return fmt.Errorf("lock statements for rollback: %w", err)
	}

	// Check if old table exists.
	exists, err := tableExists(ctx, db, "statements_old")
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("statements_old table does not exist; cannot rollback")
	}

	// Swap back.
	if _, err := tx.ExecContext(ctx, "ALTER TABLE statements RENAME TO statements_partitioned"); err != nil {
		return fmt.Errorf("rename statements to statements_partitioned: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "ALTER TABLE statements_old RENAME TO statements"); err != nil {
		return fmt.Errorf("rename statements_old to statements: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rollback transaction: %w", err)
	}
	return nil
}

// EnsureMonthPartition ensures a monthly partition exists for the period_start
// if the active statements table is partitioned.
func EnsureMonthPartition(ctx context.Context, db *sql.DB, periodStart string) error {
	partitioned, err := isTablePartitioned(ctx, db, "statements")
	if err != nil {
		return err
	}
	if !partitioned {
		return nil
	}

	t, err := time.Parse(time.RFC3339, periodStart)
	if err != nil {
		return fmt.Errorf("invalid period_start %q: %w", periodStart, err)
	}
	monthStart := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	nextMonth := monthStart.AddDate(0, 1, 0)
	partitionName := fmt.Sprintf("statements_p%d_%02d", t.Year(), t.Month())

	exists, err := partitionExists(ctx, db, partitionName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

    createQuery := fmt.Sprintf(
        "CREATE TABLE %s PARTITION OF statements FOR VALUES FROM ($1) TO ($2)",
        partitionName,
    )
    if _, err := db.ExecContext(ctx, createQuery, monthStart.Format(time.RFC3339), nextMonth.Format(time.RFC3339)); err != nil {
        return fmt.Errorf("create partition %s: %w", partitionName, err)
    }
    return nil
}

func statementsPartitioningEnabled() bool {
	val, ok := os.LookupEnv(statementsPartitionEnv)
	if !ok {
		return false
	}
	enabled, err := strconv.ParseBool(val)
	return err == nil && enabled
}

func isTablePartitioned(ctx context.Context, db *sql.DB, tableName string) (bool, error) {
    var partitioned bool
    query := `SELECT EXISTS (
        SELECT 1 FROM pg_partitioned_table
        WHERE partrelid = $1::regclass
    )`
    err := db.QueryRowContext(ctx, query, tableName).Scan(&partitioned)
    if err != nil {
        return false, fmt.Errorf("check if %s is partitioned: %w", tableName, err)
    }
    return partitioned, nil
}

func tableExists(ctx context.Context, db *sql.DB, tableName string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1)`
	err := db.QueryRowContext(ctx, query, tableName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check table %s existence: %w", tableName, err)
	}
	return exists, nil
}

func partitionExists(ctx context.Context, db *sql.DB, partitionName string) (bool, error) {
    var exists bool
    query := `SELECT EXISTS (
        SELECT 1 FROM pg_class WHERE relname = $1
    )`
    err := db.QueryRowContext(ctx, query, partitionName).Scan(&exists)
    if err != nil {
        return false, fmt.Errorf("check partition %s: %w", partitionName, err)
    }
    return exists, nil
}
