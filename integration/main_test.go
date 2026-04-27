//go:build integration

// Package integration_test contains end-to-end integration tests that validate
// route handlers against a real ephemeral Postgres database spun up via Docker.
//
// Prerequisites: Docker must be running.
//
// Run:
//
//	go test -tags integration -v -race -count=1 ./integration/...
package integration_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"stellabill-backend/internal/testutil"
)

// sharedPool is the single connection pool shared by all tests in this binary
// run. It is connected to the ephemeral Postgres container started by TestMain.
var sharedPool *pgxpool.Pool

// TestMain starts one Postgres container, applies all migrations, runs every
// test in this package, then tears the container down. The container is reused
// across tests; per-test data isolation is achieved via t.Cleanup DELETE calls.
func TestMain(m *testing.M) {
	ctx := context.Background()

	fmt.Println("==> Starting ephemeral Postgres container…")
	c, err := testutil.StartPostgresContainer(ctx)
	if err != nil {
		log.Fatalf("start postgres container: %v", err)
	}

	fmt.Println("==> Applying database migrations…")
	if err := testutil.ApplyMigrations(ctx, c.DSN); err != nil {
		_ = c.Teardown(ctx)
		log.Fatalf("apply migrations: %v", err)
	}

	fmt.Println("==> Opening shared connection pool…")
	sharedPool, err = testutil.NewPoolFromDSN(ctx, c.DSN)
	if err != nil {
		_ = c.Teardown(ctx)
		log.Fatalf("create shared pool: %v", err)
	}

	fmt.Println("==> Running integration tests…")
	code := m.Run()

	sharedPool.Close()

	// Best-effort teardown — do not mask test failures with teardown errors.
	if teardownErr := c.Teardown(ctx); teardownErr != nil {
		fmt.Fprintf(os.Stderr, "warning: container teardown: %v\n", teardownErr)
	}

	os.Exit(code)
}

