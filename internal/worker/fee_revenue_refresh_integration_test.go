//go:build integration

package worker

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// setupPostgres starts an ephemeral Postgres and applies every *.up.sql
// migration in lexicographic order by reading the files directly. We avoid the
// migrations.LoadDir runner here because the migrations directory currently
// contains duplicate version numbers (pre-existing), which that loader rejects.
func setupPostgres(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:16-alpine"),
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("postgres", connStr)
	require.NoError(t, err)

	applyMigrations(t, db)

	cleanup := func() {
		_ = db.Close()
		_ = container.Terminate(ctx)
	}
	return db, cleanup
}

func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	dir := filepath.Join("..", "..", "migrations")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var ups []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		ups = append(ups, e.Name())
	}
	sort.Strings(ups)

	for _, name := range ups {
		content, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		_, err = db.Exec(string(content))
		require.NoErrorf(t, err, "migration %s failed", name)
	}
}

func insertStatement(t *testing.T, db *sql.DB, id, customerID, issuedAt, amount, currency string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO statements (id, subscription_id, customer_id, period_start, period_end,
		                        issued_at, total_amount, currency, kind, status)
		VALUES ($1, 'sub-1', $2, $3, $3, $3, $4, $5, 'invoice', 'paid')`,
		id, customerID, issuedAt, amount, currency)
	require.NoError(t, err)
}

// TestIntegration_RefreshAggregatesByTenantAndMonth verifies the materialized
// view aggregates revenue by customer (tenant) and month, and that the worker's
// store refreshes it and records freshness.
func TestIntegration_RefreshAggregatesByTenantAndMonth(t *testing.T) {
	db, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	// Two statements in the same month for tenant A, one in another month.
	insertStatement(t, db, "s1", "tenantA", "2026-01-05T00:00:00Z", "100.00", "USD")
	insertStatement(t, db, "s2", "tenantA", "2026-01-20T00:00:00Z", "50.00", "USD")
	insertStatement(t, db, "s3", "tenantA", "2026-02-01T00:00:00Z", "25.00", "USD")
	insertStatement(t, db, "s4", "tenantB", "2026-01-10T00:00:00Z", "10.00", "USD")

	store := &sqlFeeRevenueStore{db: db}

	// First refresh must be non-concurrent (view created WITH NO DATA).
	require.NoError(t, store.Refresh(ctx, false))
	require.NoError(t, store.MarkRefreshed(ctx, time.Now().UTC()))

	type row struct {
		customer string
		count    int
		total    float64
	}
	rows, err := db.QueryContext(ctx, `
		SELECT customer_id, statement_count, total_revenue
		FROM mv_fee_revenue_monthly
		ORDER BY customer_id, month`)
	require.NoError(t, err)
	defer rows.Close()

	var got []row
	for rows.Next() {
		var r row
		require.NoError(t, rows.Scan(&r.customer, &r.count, &r.total))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())

	// tenantA Jan (2 rows, 150), tenantA Feb (1 row, 25), tenantB Jan (1 row, 10)
	require.Len(t, got, 3)
	require.Equal(t, "tenantA", got[0].customer)
	require.Equal(t, 2, got[0].count)
	require.InDelta(t, 150.0, got[0].total, 0.001)

	// Freshness recorded.
	at, ok, err := store.LastRefreshedAt(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.WithinDuration(t, time.Now().UTC(), at, time.Minute)
}

// TestIntegration_ConcurrentRefreshDoesNotBlockReads holds a long-running read
// transaction open against the view while a CONCURRENTLY refresh runs, proving
// the refresh does not block readers (stale-but-served during refresh).
func TestIntegration_ConcurrentRefreshDoesNotBlockReads(t *testing.T) {
	db, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	insertStatement(t, db, "s1", "tenantA", "2026-01-05T00:00:00Z", "100.00", "USD")

	store := &sqlFeeRevenueStore{db: db}
	require.NoError(t, store.Refresh(ctx, false)) // populate so CONCURRENTLY is allowed

	// Open a long-running read transaction and keep it open.
	readTx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer readTx.Rollback()

	var count int
	require.NoError(t, readTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mv_fee_revenue_monthly`).Scan(&count))
	require.Equal(t, 1, count)

	// Add more data, then refresh CONCURRENTLY while the read tx is still open.
	insertStatement(t, db, "s2", "tenantA", "2026-02-05T00:00:00Z", "200.00", "USD")

	refreshDone := make(chan error, 1)
	go func() {
		refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		refreshDone <- store.Refresh(refreshCtx, true) // CONCURRENTLY
	}()

	select {
	case err := <-refreshDone:
		require.NoError(t, err, "concurrent refresh should complete without being blocked")
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent refresh blocked/timed out — it must not wait on the open read tx")
	}

	// The long-running reader still sees its original snapshot (1 row).
	require.NoError(t, readTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mv_fee_revenue_monthly`).Scan(&count))
	require.Equal(t, 1, count, "open read tx should keep its snapshot")
	require.NoError(t, readTx.Commit())

	// A fresh read after refresh sees the new aggregate row.
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mv_fee_revenue_monthly`).Scan(&count))
	require.Equal(t, 2, count)
}

// TestIntegration_ArchivedAndDeletedExcluded verifies soft-deleted and archived
// statements do not contribute to the aggregate.
func TestIntegration_ArchivedAndDeletedExcluded(t *testing.T) {
	db, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	insertStatement(t, db, "live", "tenantA", "2026-01-05T00:00:00Z", "100.00", "USD")
	insertStatement(t, db, "deleted", "tenantA", "2026-01-06T00:00:00Z", "999.00", "USD")
	_, err := db.Exec(`UPDATE statements SET deleted_at = now() WHERE id = 'deleted'`)
	require.NoError(t, err)

	// Archived row: amount/date nulled, archive columns set (per migration 0010).
	insertStatement(t, db, "archived", "tenantA", "2026-01-07T00:00:00Z", "888.00", "USD")
	_, err = db.Exec(`
		UPDATE statements
		SET archived_at = now(), archive_key = 'k', total_amount = NULL, issued_at = NULL
		WHERE id = 'archived'`)
	require.NoError(t, err)

	store := &sqlFeeRevenueStore{db: db}
	require.NoError(t, store.Refresh(ctx, false))

	var total float64
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT total_revenue FROM mv_fee_revenue_monthly WHERE customer_id = 'tenantA'`).Scan(&total))
	require.InDelta(t, 100.0, total, 0.001, "only the live statement should count")
}
