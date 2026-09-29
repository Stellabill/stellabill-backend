//go:build integration

package middleware_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/testutil"

	"github.com/stretchr/testify/require"
)

func setupPostgresIdempotencyStore(t *testing.T) (context.Context, *middleware.PostgresIdempotencyStore, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	container, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)

	pool, err := testutil.NewPoolFromDSN(ctx, container.DSN)
	require.NoError(t, err)

	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "0005_create_idempotency_keys.up.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(migration))
	require.NoError(t, err)

	cleanup := func() {
		pool.Close()
		_ = container.Teardown(context.Background())
		cancel()
	}

	return ctx, middleware.NewPostgresIdempotencyStore(pool), cleanup
}

func TestPostgresIdempotencyStoreIntegration_ConcurrentGetOrInsert(t *testing.T) {
	ctx, store, cleanup := setupPostgresIdempotencyStore(t)
	defer cleanup()

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	type result struct {
		isInFlight bool
		err        error
	}
	results := make(chan result, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, _, inFlight, err := store.GetOrInsert(
				ctx,
				"tenant-a/caller-a",
				"same-key",
				"POST",
				"/api/v1/invoices",
				"hash-a",
				time.Hour,
			)
			results <- result{isInFlight: inFlight, err: err}
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	var firstRequests int
	var inFlightRequests int
	for result := range results {
		require.NoError(t, result.err)
		if result.isInFlight {
			inFlightRequests++
		} else {
			firstRequests++
		}
	}

	require.Equal(t, 1, firstRequests)
	require.Equal(t, workers-1, inFlightRequests)
}

func TestPostgresIdempotencyStoreIntegration_TTLExpiryAllowsReuse(t *testing.T) {
	ctx, store, cleanup := setupPostgresIdempotencyStore(t)
	defer cleanup()

	_, _, replay, inFlight, err := store.GetOrInsert(ctx, "scope", "ttl-key", "POST", "/one", "hash-1", 10*time.Millisecond)
	require.NoError(t, err)
	require.False(t, replay)
	require.False(t, inFlight)
	require.NoError(t, store.UpdateResponse(ctx, "scope", "ttl-key", 201, []byte(`{"ok":true}`)))

	time.Sleep(25 * time.Millisecond)

	status, body, replay, inFlight, err := store.GetOrInsert(ctx, "scope", "ttl-key", "POST", "/two", "hash-2", time.Hour)
	require.NoError(t, err)
	require.Zero(t, status)
	require.Empty(t, body)
	require.False(t, replay)
	require.False(t, inFlight)
}

func TestPostgresIdempotencyStoreIntegration_PayloadHashMismatch(t *testing.T) {
	ctx, store, cleanup := setupPostgresIdempotencyStore(t)
	defer cleanup()

	_, _, _, _, err := store.GetOrInsert(ctx, "scope", "mismatch-key", "POST", "/path", "hash-a", time.Hour)
	require.NoError(t, err)
	require.NoError(t, store.UpdateResponse(ctx, "scope", "mismatch-key", 200, []byte(`{"ok":true}`)))

	_, _, _, _, err = store.GetOrInsert(ctx, "scope", "mismatch-key", "POST", "/path", "hash-b", time.Hour)
	require.Error(t, err)
	require.True(t, errors.Is(err, middleware.ErrRequestMismatch))
}

func TestPostgresIdempotencyStoreIntegration_CancelledContextRollsBack(t *testing.T) {
	ctx, store, cleanup := setupPostgresIdempotencyStore(t)
	defer cleanup()

	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	_, _, _, _, err := store.GetOrInsert(cancelledCtx, "scope", "cancelled-key", "POST", "/path", "hash-a", time.Hour)
	require.Error(t, err)

	_, _, replay, inFlight, err := store.GetOrInsert(ctx, "scope", "cancelled-key", "POST", "/path", "hash-a", time.Hour)
	require.NoError(t, err)
	require.False(t, replay)
	require.False(t, inFlight)
}
