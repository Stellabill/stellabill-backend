package db

import (
	"context"
	"fmt"
	"time"

	"stellarbill-backend/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolPinger adapts a *pgxpool.Pool to the handlers.DBPinger interface.
//
// pgxpool.Pool exposes Ping(ctx) but the health-check code (handlers.DBPinger)
// expects PingContext(ctx). This thin wrapper bridges the two so readiness
// probes light up once a real pool is injected.
type PoolPinger struct {
	Pool *pgxpool.Pool
}

// PingContext verifies a connection can be acquired from the pool and reaches
// the database. It satisfies handlers.DBPinger.
func (p *PoolPinger) PingContext(ctx context.Context) error {
	if p == nil || p.Pool == nil {
		return fmt.Errorf("db pool not initialized")
	}
	return p.Pool.Ping(ctx)
}

// NewPoolConfig translates the validated config.Config DB pool tuning fields
// into a *pgxpool.Config. It is separated from NewPool so the mapping can be
// unit-tested without a live database.
//
// The time-based config fields are expressed in seconds; they are converted to
// time.Duration here. ConnectTimeout is applied to the per-dial timeout on the
// underlying connection config.
func NewPoolConfig(cfg config.Config) (*pgxpool.Config, error) {
	if cfg.DBConn == "" {
		return nil, fmt.Errorf("DBConn is empty")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DBConn)
	if err != nil {
		return nil, fmt.Errorf("parse database connection string: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.DBPoolMaxConns)
	poolCfg.MinConns = int32(cfg.DBPoolMinConns)
	poolCfg.MaxConnLifetime = time.Duration(cfg.DBPoolMaxConnLifetime) * time.Second
	poolCfg.MaxConnIdleTime = time.Duration(cfg.DBPoolMaxConnIdleTime) * time.Second
	poolCfg.HealthCheckPeriod = time.Duration(cfg.DBPoolHealthCheckPeriod) * time.Second

	// ConnectTimeout bounds each individual dial attempt against the database.
	if poolCfg.ConnConfig != nil {
		poolCfg.ConnConfig.ConnectTimeout = time.Duration(cfg.DBPoolConnectTimeout) * time.Second
	}

	return poolCfg, nil
}

// NewPool constructs a pgx connection pool from cfg, applying the DBPool*
// tuning fields, and verifies connectivity before returning.
//
// When cfg.DBConn is empty (e.g. local dev with no DATABASE_URL) it returns
// (nil, nil) so callers can degrade gracefully to in-memory dependencies rather
// than failing to boot.
//
// The provided ctx bounds the initial connectivity check; callers should pass a
// context with a deadline derived from cfg.DBPoolConnectTimeout.
func NewPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	if cfg.DBConn == "" {
		return nil, nil
	}

	poolCfg, err := NewPoolConfig(cfg)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	// Fail fast if the database is unreachable so startup surfaces the problem
	// rather than serving traffic against a dead pool.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
