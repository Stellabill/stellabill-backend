// Package testutil provides shared infrastructure helpers for integration tests.
// It is not build-tag gated itself; its consumers are.
package testutil

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"stellabill-backend/migrations"
)

// ContainerDSN holds the DSN and a teardown function for a running Postgres testcontainer.
type ContainerDSN struct {
	DSN      string
	Teardown func(context.Context) error
}

// StartPostgresContainer starts an ephemeral postgres:16-alpine container and
// returns its connection string together with a teardown function.
//
// The function blocks until Postgres is ready to accept connections (up to 60 s).
// WithOccurrence(2) is intentional: Postgres emits the ready log message twice
// during startup; waiting for the second occurrence avoids a startup race where
// the first log appears before connections are truly accepted.
func StartPostgresContainer(ctx context.Context) (*ContainerDSN, error) {
	container, err := tcpostgres.RunContainer(ctx,
		tcpostgres.WithDatabase("stellarbill_test"),
		tcpostgres.WithUsername("testuser"),
		tcpostgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("start postgres container: %w", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("get connection string: %w", err)
	}

	return &ContainerDSN{
		DSN: dsn,
		Teardown: func(ctx context.Context) error {
			return container.Terminate(ctx)
		},
	}, nil
}

// ApplyMigrations runs all embedded *.sql files in lexicographic order against
// the database at dsn. Each file runs in its own transaction; if a file fails,
// the transaction is rolled back and the error is returned immediately.
func ApplyMigrations(ctx context.Context, dsn string) error {
	pool, err := NewPoolFromDSN(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open pool for migrations: %w", err)
	}
	defer pool.Close()

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	// Guarantee lexicographic order (001 before 002, etc.).
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		content, err := migrations.FS.ReadFile(entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin transaction for %s: %w", entry.Name(), err)
		}

		if _, execErr := tx.Exec(ctx, string(content)); execErr != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("exec migration %s: %w", entry.Name(), execErr)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// NewPoolFromDSN creates a pgxpool.Pool connected to dsn with sensible defaults
// (max 5 connections, 5 s connect timeout).
func NewPoolFromDSN(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}
	cfg.MaxConns = 5
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// Verify connectivity.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

