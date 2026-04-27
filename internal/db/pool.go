// Package db provides the production database connection pool factory.
//
// Design decisions
// ----------------
//   - pgxpool is used (jackc/pgx/v5) because it is already the project's
//     driver and exposes richer pool statistics than database/sql.
//   - All tuning knobs are driven by config.Config so they can be changed
//     via environment variables without recompiling.
//   - A background goroutine scrapes pool.Stat() on a configurable interval
//     and pushes the values into the Prometheus gauges defined in
//     internal/metrics/pool.go.  The goroutine is stopped when the returned
//     cancel function is called (typically in the graceful-shutdown callback).
//   - ConnectTimeout is enforced at the pgx driver level so a slow Postgres
//     host never blocks startup indefinitely.
//   - MaxConnLifetime + MaxConnIdleTime together prevent stale connections
//     from accumulating after a partial network outage or a Postgres restart.
//
// Leak-detection signals
// ----------------------
//   - db_pool_acquired_conns      — connections currently checked out
//   - db_pool_idle_conns          — connections sitting idle
//   - db_pool_total_conns         — total connections in the pool
//   - db_pool_max_conns           — configured ceiling (constant, useful for ratio alerts)
//   - db_pool_acquire_count_total — cumulative acquires (rate = throughput)
//   - db_pool_acquire_duration_seconds — histogram of wait time before a
//     connection is handed to the caller; a rising p99 is the earliest
//     signal of pool saturation.
//   - db_pool_canceled_acquire_total — acquires that timed-out/were cancelled;
//     non-zero in production means the pool ceiling is too low.
//
// Alerting guidance (see docs/db-pool-tuning.md for full runbook):
//
//	alert: DBPoolSaturated
//	  expr: db_pool_acquired_conns / db_pool_max_conns > 0.85
//
//	alert: DBPoolAcquireLatencyHigh
//	  expr: histogram_quantile(0.99, db_pool_acquire_duration_seconds) > 0.5
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/metrics"
)

// Pool is an alias so callers import only this package.
type Pool = pgxpool.Pool

// Open creates a production-ready pgxpool.Pool from cfg, verifies connectivity
// with a Ping, and starts the background metrics scraper.
//
// The caller MUST call the returned stop function (e.g. in a shutdown callback)
// to stop the scraper goroutine and close the pool.
//
//	pool, stop, err := db.Open(ctx, cfg)
//	if err != nil { ... }
//	defer stop()
func Open(ctx context.Context, cfg config.Config) (*Pool, func(), error) {
	poolCfg, err := buildPoolConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("db.Open: build pool config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("db.Open: create pool: %w", err)
	}

	// Fail fast — surface misconfigurations before the server starts serving.
	pingCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.DBPoolConnectTimeout)*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("db.Open: ping: %w", err)
	}

	stopScraper := metrics.StartPoolScraper(pool, time.Duration(cfg.DBPoolMetricsInterval)*time.Second)

	stop := func() {
		stopScraper()
		pool.Close()
	}

	return pool, stop, nil
}

// buildPoolConfig translates config.Config into a pgxpool.Config.
// Kept separate so it can be unit-tested without a real Postgres instance.
func buildPoolConfig(cfg config.Config) (*pgxpool.Config, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DBConn)
	if err != nil {
		return nil, fmt.Errorf("parse DSN: %w", err)
	}

	// --- connection count limits ---
	poolCfg.MaxConns = int32(cfg.DBPoolMaxConns)
	poolCfg.MinConns = int32(cfg.DBPoolMinConns)

	// --- lifetime / idle eviction ---
	// MaxConnLifetime: recycle connections periodically so load-balancer
	// stickiness doesn't pin all traffic to one Postgres backend.
	poolCfg.MaxConnLifetime = time.Duration(cfg.DBPoolMaxConnLifetime) * time.Second
	// MaxConnLifetimeJitter: spread recycling across the interval to avoid a
	// thundering-herd of simultaneous reconnects.
	poolCfg.MaxConnLifetimeJitter = time.Duration(cfg.DBPoolMaxConnLifetime) * time.Second / 10
	// MaxConnIdleTime: evict connections that have been idle longer than this.
	// Keeps the pool lean during off-peak hours and avoids "connection reset"
	// errors from firewalls that silently drop idle TCP sessions.
	poolCfg.MaxConnIdleTime = time.Duration(cfg.DBPoolMaxConnIdleTime) * time.Second

	// --- acquire timeout ---
	// HealthCheckPeriod: how often pgxpool proactively checks idle connections.
	poolCfg.HealthCheckPeriod = time.Duration(cfg.DBPoolHealthCheckPeriod) * time.Second

	// ConnectTimeout is set on the underlying ConnConfig so it applies to
	// every individual dial attempt, not just the initial pool creation.
	poolCfg.ConnConfig.ConnectTimeout = time.Duration(cfg.DBPoolConnectTimeout) * time.Second

	return poolCfg, nil
}
