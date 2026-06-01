//go:build integration

package middleware

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"stellarbill-backend/internal/migrations"
)

func setupTestPostgres(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	ctx := context.Background()
	pgContainer, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:15-alpine"),
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(5*time.Second),
		),
	)
	require.NoError(t, err)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("postgres", connStr)
	require.NoError(t, err)

	migs, err := migrations.LoadDir("../../migrations")
	require.NoError(t, err)

	runner := migrations.Runner{DB: db}
	_, err = runner.Up(ctx, migs)
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)

	cleanup := func() {
		pool.Close()
		_ = db.Close()
		_ = pgContainer.Terminate(ctx)
	}

	return pool, cleanup
}

func TestPostgresIdempotencyStore_GetOrInsert_Concurrent(t *testing.T) {
	pool, cleanup := setupTestPostgres(t)
	defer cleanup()

	store := NewPostgresIdempotencyStore(pool)
	ctx := context.Background()

	const scope = "test-scope"
	const key = "concurrent-key"
	const method = "POST"
	const path = "/test"
	const hash = "hash1"
	const ttl = time.Hour

	var wg sync.WaitGroup
	var results [2]struct {
		statusCode int
		body       []byte
		isReplay   bool
		isInFlight bool
		err        error
	}

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx].statusCode, results[idx].body, results[idx].isReplay, results[idx].isInFlight, results[idx].err = store.GetOrInsert(ctx, scope, key, method, path, hash, ttl)
		}(i)
	}

	wg.Wait()

	// One should succeed (isInFlight=false), the other should be in flight
	var inFlightCount int
	for _, r := range results {
		require.NoError(t, r.err)
		if r.isInFlight {
			inFlightCount++
		}
	}
	assert.Equal(t, 1, inFlightCount)
}

func TestPostgresIdempotencyStore_GetOrInsert_Expiration(t *testing.T) {
	pool, cleanup := setupTestPostgres(t)
	defer cleanup()

	store := NewPostgresIdempotencyStore(pool)
	ctx := context.Background()

	const scope = "test-scope"
	const key = "expire-key"
	const method = "POST"
	const path = "/test"
	const hash = "hash1"
	const ttl = 50 * time.Millisecond

	// First insert
	statusCode, body, isReplay, isInFlight, err := store.GetOrInsert(ctx, scope, key, method, path, hash, ttl)
	require.NoError(t, err)
	assert.Equal(t, 0, statusCode)
	assert.False(t, isReplay)
	assert.False(t, isInFlight)

	// Update response
	err = store.UpdateResponse(ctx, scope, key, 200, []byte("response"))
	require.NoError(t, err)

	// Should replay
	statusCode, body, isReplay, isInFlight, err = store.GetOrInsert(ctx, scope, key, method, path, hash, ttl)
	require.NoError(t, err)
	assert.Equal(t, 200, statusCode)
	assert.Equal(t, []byte("response"), body)
	assert.True(t, isReplay)
	assert.False(t, isInFlight)

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be new
	statusCode, body, isReplay, isInFlight, err = store.GetOrInsert(ctx, scope, key, method, path, hash, ttl)
	require.NoError(t, err)
	assert.Equal(t, 0, statusCode)
	assert.False(t, isReplay)
	assert.False(t, isInFlight)
}

func TestPostgresIdempotencyStore_GetOrInsert_RequestMismatch(t *testing.T) {
	pool, cleanup := setupTestPostgres(t)
	defer cleanup()

	store := NewPostgresIdempotencyStore(pool)
	ctx := context.Background()

	const scope = "test-scope"
	const key = "mismatch-key"
	const method = "POST"
	const path = "/test"
	const hash1 = "hash1"
	const hash2 = "hash2"
	const ttl = time.Hour

	// First insert
	statusCode, body, isReplay, isInFlight, err := store.GetOrInsert(ctx, scope, key, method, path, hash1, ttl)
	require.NoError(t, err)
	assert.Equal(t, 0, statusCode)
	assert.False(t, isReplay)
	assert.False(t, isInFlight)

	// Update response
	err = store.UpdateResponse(ctx, scope, key, 200, []byte("response"))
	require.NoError(t, err)

	// Try with different hash
	_, _, _, _, err = store.GetOrInsert(ctx, scope, key, method, path, hash2, ttl)
	assert.ErrorIs(t, err, ErrRequestMismatch)
}

func TestPostgresIdempotencyStore_Delete(t *testing.T) {
	pool, cleanup := setupTestPostgres(t)
	defer cleanup()

	store := NewPostgresIdempotencyStore(pool)
	ctx := context.Background()

	const scope = "test-scope"
	const key = "delete-key"
	const method = "POST"
	const path = "/test"
	const hash = "hash1"
	const ttl = time.Hour

	// Insert
	_, _, _, _, err := store.GetOrInsert(ctx, scope, key, method, path, hash, ttl)
	require.NoError(t, err)

	// Delete
	err = store.Delete(ctx, scope, key)
	require.NoError(t, err)

	// Should be new
	statusCode, _, isReplay, _, err := store.GetOrInsert(ctx, scope, key, method, path, hash, ttl)
	require.NoError(t, err)
	assert.Equal(t, 0, statusCode)
	assert.False(t, isReplay)
}

func TestPostgresIdempotencyStore_ContextCancellation(t *testing.T) {
	pool, cleanup := setupTestPostgres(t)
	defer cleanup()

	store := NewPostgresIdempotencyStore(pool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	const scope = "test-scope"
	const key = "cancel-key"
	const method = "POST"
	const path = "/test"
	const hash = "hash1"
	const ttl = time.Hour

	_, _, _, _, err := store.GetOrInsert(ctx, scope, key, method, path, hash, ttl)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
