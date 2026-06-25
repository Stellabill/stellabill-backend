package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPinger implements Pinger interface for testing.
type mockPinger struct {
	pingErr error
}

func (m *mockPinger) PingContext(ctx context.Context) error {
	return m.pingErr
}

// mockDBTX implements DBTX for testing and counts calls.
type mockDBTX struct {
	*mockPinger
	execCount        int
	prepareCount     int
	queryCount       int
	queryRowCount    int
	execNoCtxCount   int
	queryNoCtxCount  int
	queryRowNoCtxCount int
}

func newMockDBTX(pingErr error) *mockDBTX {
	return &mockDBTX{
		mockPinger: &mockPinger{pingErr: pingErr},
	}
}

func (m *mockDBTX) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	m.execCount++
	return sqlmock.NewResult(1, 1), nil
}

func (m *mockDBTX) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	m.prepareCount++
	return nil, nil
}

func (m *mockDBTX) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	m.queryCount++
	return nil, nil
}

func (m *mockDBTX) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	m.queryRowCount++
	return &sql.Row{}
}

func (m *mockDBTX) Exec(query string, args ...any) (sql.Result, error) {
	m.execNoCtxCount++
	return sqlmock.NewResult(1, 1), nil
}

func (m *mockDBTX) Query(query string, args ...any) (*sql.Rows, error) {
	m.queryNoCtxCount++
	return nil, nil
}

func (m *mockDBTX) QueryRow(query string, args ...any) *sql.Row {
	m.queryRowNoCtxCount++
	return &sql.Row{}
}

func TestReadRouter_ReaderRouting(t *testing.T) {
	primary := newMockDBTX(nil)
	replica := newMockDBTX(nil)
	router := NewReadRouter(primary, replica)

	t.Run("selects replica when no freshness token is present", func(t *testing.T) {
		ctx := context.Background()
		selected := router.Reader(ctx)
		assert.Equal(t, replica, selected)
	})

	t.Run("selects primary when freshness token is present", func(t *testing.T) {
		ctx := WithFreshnessToken(context.Background(), "token-123")
		selected := router.Reader(ctx)
		assert.Equal(t, primary, selected)
	})

	t.Run("selects primary when replica is nil", func(t *testing.T) {
		nilReplicaRouter := NewReadRouter(primary, nil)
		selected := nilReplicaRouter.Reader(context.Background())
		assert.Equal(t, primary, selected)
	})
}

func TestReadRouter_ReplicaFailover(t *testing.T) {
	primary := newMockDBTX(nil)
	replica := newMockDBTX(errors.New("connection failed"))
	router := NewReadRouter(primary, replica)
	router.healthCheckFreq = 10 * time.Millisecond // short check interval for testing

	t.Run("falls back to primary when replica ping fails", func(t *testing.T) {
		ctx := context.Background()
		// First call triggers health check and marks it down
		selected := router.Reader(ctx)
		assert.Equal(t, primary, selected)
		
		// Second call within healthCheckFreq uses cached "down" state
		selected = router.Reader(ctx)
		assert.Equal(t, primary, selected)
	})

	t.Run("recovers once replica comes back up", func(t *testing.T) {
		ctx := context.Background()
		// Force check to trip replica as down first
		router.Reader(ctx)
		
		// Wait for recovery check window
		time.Sleep(20 * time.Millisecond)
		
		// Make replica healthy
		replica.pingErr = nil
		
		// Next call should re-check and route to replica
		selected := router.Reader(ctx)
		assert.Equal(t, replica, selected)
	})
}

func TestReadRouter_DBTXInterfaceMethods(t *testing.T) {
	primary := newMockDBTX(nil)
	replica := newMockDBTX(nil)
	router := NewReadRouter(primary, replica)
	ctx := context.Background()

	t.Run("ExecContext always targets primary", func(t *testing.T) {
		_, err := router.ExecContext(ctx, "INSERT INTO table VALUES(1)")
		require.NoError(t, err)
		assert.Equal(t, 1, primary.execCount)
		assert.Equal(t, 0, replica.execCount)
	})

	t.Run("PrepareContext always targets primary", func(t *testing.T) {
		_, err := router.PrepareContext(ctx, "SELECT * FROM table WHERE id = $1")
		require.NoError(t, err)
		assert.Equal(t, 1, primary.prepareCount)
		assert.Equal(t, 0, replica.prepareCount)
	})

	t.Run("QueryContext targets replica when safe", func(t *testing.T) {
		_, err := router.QueryContext(ctx, "SELECT * FROM table")
		require.NoError(t, err)
		assert.Equal(t, 0, primary.queryCount)
		assert.Equal(t, 1, replica.queryCount)
	})

	t.Run("QueryRowContext targets replica when safe", func(t *testing.T) {
		_ = router.QueryRowContext(ctx, "SELECT * FROM table LIMIT 1")
		assert.Equal(t, 0, primary.queryRowCount)
		assert.Equal(t, 1, replica.queryRowCount)
	})

	t.Run("QueryContext targets primary when freshness token present", func(t *testing.T) {
		freshCtx := WithFreshnessToken(ctx, "fresh")
		_, err := router.QueryContext(freshCtx, "SELECT * FROM table")
		require.NoError(t, err)
		assert.Equal(t, 1, primary.queryCount)
		assert.Equal(t, 1, replica.queryCount) // unchanged
	})

	t.Run("context-less methods target primary", func(t *testing.T) {
		_, _ = router.Exec("INSERT INTO table VALUES(1)")
		_, _ = router.Query("SELECT * FROM table")
		_ = router.QueryRow("SELECT * FROM table LIMIT 1")

		assert.Equal(t, 1, primary.execNoCtxCount)
		assert.Equal(t, 1, primary.queryNoCtxCount)
		assert.Equal(t, 1, primary.queryRowNoCtxCount)

		assert.Equal(t, 0, replica.execNoCtxCount)
		assert.Equal(t, 0, replica.queryNoCtxCount)
		assert.Equal(t, 0, replica.queryRowNoCtxCount)
	})
}
