package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"stellarbill-backend/internal/config"
	"stellarbill-backend/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

// applyMigrationsOnStartup runs all embedded SQL migrations against the
// configured database before the server begins serving traffic. Each file
// executes in its own transaction; a failing migration aborts startup with an
// error.
func applyMigrationsOnStartup(cfg *config.Config) error {
	if cfg == nil || cfg.DBConn == "" {
		return fmt.Errorf("applyMigrationsOnStartup: empty database connection string")
	}

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("applyMigrationsOnStartup: read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("applyMigrationsOnStartup: no migration files found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	poolCfg, err := pgxpool.ParseConfig(cfg.DBConn)
	if err != nil {
		return fmt.Errorf("applyMigrationsOnStartup: parse DSN: %w", err)
	}
	poolCfg.ConnConfig.ConnectTimeout = 5 * time.Second
	poolCfg.MaxConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("applyMigrationsOnStartup: create pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("applyMigrationsOnStartup: connect: %w", err)
	}

	for _, name := range files {
		content, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("applyMigrationsOnStartup: read %s: %w", name, err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("applyMigrationsOnStartup: begin %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("applyMigrationsOnStartup: exec %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("applyMigrationsOnStartup: commit %s: %w", name, err)
		}
	}

	return nil
}
