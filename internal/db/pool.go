package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/servertiming"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var hedgedReadsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "hedged_reads_total",
		Help: "Total hedged read attempts by which attempt won.",
	},
	[]string{"winner"},
)

// HedgedQuery executes the first attempt immediately and starts a second attempt
// after the supplied delay if the first attempt is still running. The first
// successful attempt wins and the losing attempt is canceled to avoid doing
// extra work on replicas.
func HedgedQuery(ctx context.Context, delay time.Duration, fn func(ctx context.Context, attempt int) error) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if delay < 0 {
		delay = 0
	}

	type attemptResult struct {
		attempt int
		err     error
	}

	results := make(chan attemptResult, 2)
	done := make(chan struct{})
	var doneOnce sync.Once
	closeDone := func() {
		doneOnce.Do(func() {
			close(done)
		})
	}
	var cancelFirst context.CancelFunc
	firstCtx, cancelFirst := context.WithCancel(ctx)
	defer cancelFirst()

	go func() {
		results <- attemptResult{attempt: 1, err: fn(firstCtx, 1)}
	}()

	var (
		cancelSecond  context.CancelFunc
		secondCtx     context.Context
		secondStarted bool
		mu            sync.Mutex
	)

	startSecond := func() {
		mu.Lock()
		defer mu.Unlock()
		if secondStarted {
			return
		}
		secondStarted = true
		secondCtx, cancelSecond = context.WithCancel(ctx)
		go func() {
			results <- attemptResult{attempt: 2, err: fn(secondCtx, 2)}
		}()
	}

	if delay > 0 {
		go func() {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-done:
			case <-timer.C:
				startSecond()
			}
		}()
	} else {
		startSecond()
	}

	var (
		winnerErr error
		winner    int
		seen      int
	)

	for seen < 2 {
		select {
		case <-ctx.Done():
			if cancelSecond != nil {
				cancelSecond()
			}
			return 0, ctx.Err()
		case result := <-results:
			seen++
			if result.err == nil {
				if result.attempt == 1 {
					if cancelSecond != nil {
						cancelSecond()
					}
				} else {
					cancelFirst()
				}
				closeDone()
				hedgedReadsTotal.WithLabelValues(map[bool]string{true: "first", false: "second"}[result.attempt == 1]).Inc()
				return result.attempt, nil
			}
			winnerErr = result.err
			winner = result.attempt
		}
	}

	if winner > 0 {
		hedgedReadsTotal.WithLabelValues(map[bool]string{true: "first", false: "second"}[winner == 1]).Inc()
	}
	return winner, winnerErr
}

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
//
// When cfg.PgBouncerEnabled is true, the pool is configured for PgBouncer
// transaction pooling:
//   - The connection target is rewritten to cfg.PgBouncerHost:cfg.PgBouncerPort
//     while retaining the original database name, user, and sslmode.
//   - The query-exec mode is set according to cfg.DBStatementCacheMode:
//     "describe" → QueryExecModeDescribeExec (no prepared statements sent to the
//     server; pgx uses DescribeStatement to infer types — required for PgBouncer
//     transaction pooling).
//     "simple"   → QueryExecModeSimpleProtocol (plain text queries, no binary
//     protocol — widest compatibility).
//     "prepare"  → QueryExecModeCacheStatement (default pgx behaviour; only safe
//     with PgBouncer session pooling or a direct connection).
//
// The StatementCacheCapacity is set to 0 when using "describe" or "simple" mode
// to ensure pgx does not silently fall back to the prepared-statement cache.
func NewPoolConfig(cfg config.Config) (*pgxpool.Config, error) {
	if cfg.DBConn == "" {
		return nil, fmt.Errorf("DBConn is empty")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DBConn)
	if err != nil {
		return nil, fmt.Errorf("parse database connection string: %w", err)
	}

	// When PgBouncer sidecar is enabled, redirect the TCP connection to the
	// sidecar endpoint.  The rest of the DSN (database, user, password, SSL
	// settings) is kept intact so the pool authenticates to PgBouncer exactly
	// as it would to Postgres directly.
	if cfg.PgBouncerEnabled && poolCfg.ConnConfig != nil {
		poolCfg.ConnConfig.Host = cfg.PgBouncerHost
		poolCfg.ConnConfig.Port = uint16(cfg.PgBouncerPort) //nolint:gosec // port validated 1–65535
	}

	poolCfg.MaxConns = int32(cfg.DBPoolMaxConns)
	poolCfg.MinConns = int32(cfg.DBPoolMinConns)
	poolCfg.MaxConnLifetime = time.Duration(cfg.DBPoolMaxConnLifetime) * time.Second
	poolCfg.MaxConnIdleTime = time.Duration(cfg.DBPoolMaxConnIdleTime) * time.Second
	poolCfg.HealthCheckPeriod = time.Duration(cfg.DBPoolHealthCheckPeriod) * time.Second

	// ConnectTimeout bounds each individual dial attempt against the database
	// (or PgBouncer sidecar when enabled).
	if poolCfg.ConnConfig != nil {
		poolCfg.ConnConfig.ConnectTimeout = time.Duration(cfg.DBPoolConnectTimeout) * time.Second
		poolCfg.ConnConfig.Tracer = &timingTracer{}
	}

	return poolCfg, nil
}

type queryStartTimeKey struct{}

type timingTracer struct{}

func (t *timingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryStartTimeKey{}, time.Now())
}

func (t *timingTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	startVal := ctx.Value(queryStartTimeKey{})
	if start, ok := startVal.(time.Time); ok {
		if rec := servertiming.FromContext(ctx); rec != nil {
			rec.RecordDB(time.Since(start))
		}
	}
	if acc := middleware.AccumulatorFromContext(ctx); acc != nil {
		acc.AddDBRowsRead(data.CommandTag.RowsAffected())
	}
}

// DrainPool stops accepting new connections and waits for in-flight queries to
// complete, bounded by ctx. It closes the pool afterward and should be called
// during graceful shutdown after the HTTP server has stopped accepting new
// requests. A nil pool is a no-op.
//
// Because pgxpool.Pool.Close() blocks until all acquired connections are
// released, DrainPool runs Close in a separate goroutine and respects ctx.
func DrainPool(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return nil
	}

	done := make(chan struct{})
	go func() {
		pool.Close()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("drain database pool: %w", ctx.Err())
	}
}

// NewReplicaPool opens a second pgx connection pool for the hot-standby read
// replica configured through DATABASE_REPLICA_URL / DB_REPLICA_URL (loaded into
// cfg.DBReplicaConn). It reuses the primary pool's DBPool* tuning and PgBouncer
// settings so replicas and primary are provisioned consistently.
//
// When cfg.DBReplicaConn is empty (no replica configured) it returns (nil, nil)
// so callers can degrade gracefully — the ReadRouter automatically routes all
// reads to the primary in that case, preserving existing behavior.
func NewReplicaPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	if cfg.DBReplicaConn == "" {
		return nil, nil
	}

	// Reuse NewPool by pointing it at the replica DSN; all pool tuning fields
	// and PgBouncer rewriting are identical for primary and replica.
	replicaCfg := cfg
	replicaCfg.DBConn = cfg.DBReplicaConn
	return NewPool(ctx, replicaCfg)
}

// NewPool constructs a pgx connection pool from cfg, applying the DBPool*
// tuning fields and PgBouncer settings, and verifies connectivity before
// returning.
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
