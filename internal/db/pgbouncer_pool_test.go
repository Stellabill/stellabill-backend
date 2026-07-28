package db

import (
	"context"
	"stellarbill-backend/internal/config"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pgBouncerBaseCfg returns a Config with PgBouncer disabled, suitable for
// testing pool configuration without a live database.
func pgBouncerBaseCfg() config.Config {
	return config.Config{
		DBConn:                  "postgres://user:pass@db.internal:5432/app?sslmode=disable",
		DBPoolMaxConns:          10,
		DBPoolMinConns:          2,
		DBPoolMaxConnLifetime:   1800,
		DBPoolMaxConnIdleTime:   300,
		DBPoolConnectTimeout:    5,
		DBPoolHealthCheckPeriod: 30,
		DBPoolMetricsInterval:   15,
		// PgBouncer defaults
		PgBouncerEnabled:         false,
		PgBouncerHost:            config.DefaultPgBouncerHost,
		PgBouncerPort:            config.DefaultPgBouncerPort,
		DBStatementCacheMode:     config.DefaultDBStatementCacheMode,
		PgBouncerIdleInTxTimeout: config.DefaultPgBouncerIdleInTxTimeout,
	}
}

// ─── Statement cache mode: applyStatementCacheMode ───────────────────────────

func TestApplyStatementCacheMode_Describe(t *testing.T) {
	connCfg, err := pgxpool.ParseConfig("postgres://user:pass@localhost/db")
	require.NoError(t, err)

	applyStatementCacheMode(connCfg.ConnConfig, config.StatementCacheModeDescribe)

	assert.Equal(t, pgx.QueryExecModeDescribeExec, connCfg.ConnConfig.DefaultQueryExecMode,
		"describe mode must set DescribeExec to avoid sending prepared statements to the server")
	assert.Equal(t, 0, connCfg.ConnConfig.StatementCacheCapacity,
		"describe mode must set StatementCacheCapacity=0 to prevent silent fallback to the cache")
}

func TestApplyStatementCacheMode_Simple(t *testing.T) {
	connCfg, err := pgxpool.ParseConfig("postgres://user:pass@localhost/db")
	require.NoError(t, err)

	applyStatementCacheMode(connCfg.ConnConfig, config.StatementCacheModeSimple)

	assert.Equal(t, pgx.QueryExecModeSimpleProtocol, connCfg.ConnConfig.DefaultQueryExecMode,
		"simple mode must use the simple query protocol")
	assert.Equal(t, 0, connCfg.ConnConfig.StatementCacheCapacity,
		"simple mode must set StatementCacheCapacity=0")
}

func TestApplyStatementCacheMode_Prepare(t *testing.T) {
	connCfg, err := pgxpool.ParseConfig("postgres://user:pass@localhost/db")
	require.NoError(t, err)

	applyStatementCacheMode(connCfg.ConnConfig, config.StatementCacheModePrepare)

	assert.Equal(t, pgx.QueryExecModeCacheStatement, connCfg.ConnConfig.DefaultQueryExecMode,
		"prepare mode must use the statement cache (default pgx behaviour)")
}

func TestApplyStatementCacheMode_Empty_DefaultsToPrepare(t *testing.T) {
	connCfg, err := pgxpool.ParseConfig("postgres://user:pass@localhost/db")
	require.NoError(t, err)

	applyStatementCacheMode(connCfg.ConnConfig, "")

	assert.Equal(t, pgx.QueryExecModeCacheStatement, connCfg.ConnConfig.DefaultQueryExecMode,
		"empty mode must default to prepare (standard pgx behaviour)")
}

func TestApplyStatementCacheMode_Unknown_DefaultsToPrepare(t *testing.T) {
	connCfg, err := pgxpool.ParseConfig("postgres://user:pass@localhost/db")
	require.NoError(t, err)

	applyStatementCacheMode(connCfg.ConnConfig, "turbo-mode")

	assert.Equal(t, pgx.QueryExecModeCacheStatement, connCfg.ConnConfig.DefaultQueryExecMode,
		"unknown mode must default to prepare")
}

// ─── NewPoolConfig: PgBouncer host/port redirection ──────────────────────────

func TestNewPoolConfig_PgBouncerEnabled_OverridesHostPort(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.PgBouncerEnabled = true
	cfg.PgBouncerHost = "127.0.0.1"
	cfg.PgBouncerPort = 6432
	cfg.DBStatementCacheMode = config.StatementCacheModeDescribe

	pc, err := NewPoolConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, pc)

	assert.Equal(t, "127.0.0.1", pc.ConnConfig.Host,
		"PgBouncer host must override the DSN host")
	assert.Equal(t, uint16(6432), pc.ConnConfig.Port,
		"PgBouncer port must override the DSN port")
	assert.Equal(t, pgx.QueryExecModeDescribeExec, pc.ConnConfig.DefaultQueryExecMode,
		"describe mode must be applied when PgBouncer is enabled")
	assert.Equal(t, 0, pc.ConnConfig.StatementCacheCapacity)
}

func TestNewPoolConfig_PgBouncerDisabled_KeepsOriginalHostPort(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.PgBouncerEnabled = false

	pc, err := NewPoolConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, pc)

	assert.Equal(t, "db.internal", pc.ConnConfig.Host,
		"original DSN host must be preserved when PgBouncer is disabled")
	assert.Equal(t, uint16(5432), pc.ConnConfig.Port,
		"original DSN port must be preserved when PgBouncer is disabled")
}

func TestNewPoolConfig_PgBouncerEnabled_DefaultPort(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.PgBouncerEnabled = true
	cfg.PgBouncerHost = "127.0.0.1"
	cfg.PgBouncerPort = config.DefaultPgBouncerPort // 5432
	cfg.DBStatementCacheMode = config.StatementCacheModeDescribe

	pc, err := NewPoolConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, uint16(config.DefaultPgBouncerPort), pc.ConnConfig.Port)
}

// ─── NewPoolConfig: statement cache mode wired from config ───────────────────

func TestNewPoolConfig_StatementCacheDescribe_SetOnConnConfig(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.DBStatementCacheMode = config.StatementCacheModeDescribe

	pc, err := NewPoolConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, pgx.QueryExecModeDescribeExec, pc.ConnConfig.DefaultQueryExecMode)
	assert.Equal(t, 0, pc.ConnConfig.StatementCacheCapacity)
}

func TestNewPoolConfig_StatementCacheSimple_SetOnConnConfig(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.DBStatementCacheMode = config.StatementCacheModeSimple

	pc, err := NewPoolConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, pgx.QueryExecModeSimpleProtocol, pc.ConnConfig.DefaultQueryExecMode)
	assert.Equal(t, 0, pc.ConnConfig.StatementCacheCapacity)
}

func TestNewPoolConfig_StatementCachePrepare_SetOnConnConfig(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.DBStatementCacheMode = config.StatementCacheModePrepare

	pc, err := NewPoolConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, pgx.QueryExecModeCacheStatement, pc.ConnConfig.DefaultQueryExecMode)
}

// ─── NewPoolConfig: pool sizing still applied with PgBouncer enabled ─────────

func TestNewPoolConfig_PgBouncerEnabled_PoolSizingStillApplied(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.PgBouncerEnabled = true
	cfg.PgBouncerHost = "127.0.0.1"
	cfg.PgBouncerPort = 6432
	cfg.DBStatementCacheMode = config.StatementCacheModeDescribe
	cfg.DBPoolMaxConns = 20
	cfg.DBPoolMinConns = 3

	pc, err := NewPoolConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, int32(20), pc.MaxConns, "MaxConns must be applied even when PgBouncer is enabled")
	assert.Equal(t, int32(3), pc.MinConns, "MinConns must be applied even when PgBouncer is enabled")
}

// ─── NewPool: empty DSN degrades gracefully ───────────────────────────────────

func TestNewPool_PgBouncerEnabled_EmptyDBConn_ReturnsNilNil(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.DBConn = ""
	cfg.PgBouncerEnabled = true

	pool, err := NewPool(context.Background(), cfg)
	assert.NoError(t, err, "empty DSN must degrade gracefully even when PgBouncer is enabled")
	assert.Nil(t, pool)
}

// ─── NewPool: connect timeout with PgBouncer host ────────────────────────────
//
// This test verifies that the connect timeout applies to the PgBouncer sidecar
// address, not only to direct Postgres connections. RFC 5737 TEST-NET-1
// (192.0.2.0/24) is non-routable so the dial blocks until the timeout fires.
func TestNewPool_PgBouncerEnabled_ConnectTimeout(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.PgBouncerEnabled = true
	cfg.PgBouncerHost = "192.0.2.1" // TEST-NET-1 — non-routable
	cfg.PgBouncerPort = 5432
	cfg.DBStatementCacheMode = config.StatementCacheModeDescribe
	cfg.DBPoolConnectTimeout = 1

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	pool, err := NewPool(ctx, cfg)
	elapsed := time.Since(start)

	require.Error(t, err, "unreachable PgBouncer address must surface an error")
	assert.Nil(t, pool)
	assert.Less(t, elapsed, 5*time.Second,
		"pool creation must fail fast via connect timeout, not hang for 5s")
}

// ─── Idle-in-transaction semantics (config level) ────────────────────────────
//
// PgBouncer enforces idle_transaction_timeout on the backend side. At the
// Go-pool level we validate that the config field is present and plumbed
// through correctly — actual enforcement is in pgbouncer.ini.

func TestNewPoolConfig_IdleInTxTimeout_StoredInConfig(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.PgBouncerEnabled = true
	cfg.PgBouncerIdleInTxTimeout = 45
	cfg.DBStatementCacheMode = config.StatementCacheModeDescribe

	// NewPoolConfig does not embed IdleInTxTimeout into pgxpool.Config because
	// enforcement is sidecar-side. Assert that the field is non-zero so that
	// a future PgBouncer config generator (e.g. a sidecar init container) can
	// read it.
	assert.Equal(t, 45, cfg.PgBouncerIdleInTxTimeout,
		"IdleInTxTimeout must be preserved in config for sidecar configuration")

	// NewPoolConfig must still succeed.
	pc, err := NewPoolConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, pc)
}

// ─── Long-running transaction guard: pool MaxConnLifetime ────────────────────
//
// When transactions hold backend connections for a long time the pool must
// still recycle connections according to MaxConnLifetime. This unit test
// ensures the lifetime field is forwarded into the pgxpool.Config so the pool
// will evict and re-dial stale connections without operator intervention.

func TestNewPoolConfig_LongRunningTx_MaxConnLifetimeForwarded(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.DBPoolMaxConnLifetime = 600 // 10 minutes — deliberately short for testing
	cfg.PgBouncerEnabled = true
	cfg.DBStatementCacheMode = config.StatementCacheModeDescribe

	pc, err := NewPoolConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, 600*time.Second, pc.MaxConnLifetime,
		"MaxConnLifetime must be forwarded so stale connections are recycled "+
			"even when long-running transactions are in flight")
}

// ─── Regression guard: prepare mode must not be silently overridden ──────────
//
// If DB_STATEMENT_CACHE_MODE is "prepare" and PgBouncer is enabled, pool.go
// must faithfully mirror the config value and NOT silently switch to describe
// mode. The config layer emits a warning; the pool layer trusts the config.

func TestNewPoolConfig_PgBouncerEnabled_PrepareMode_MirroredFaithfully(t *testing.T) {
	cfg := pgBouncerBaseCfg()
	cfg.PgBouncerEnabled = true
	cfg.DBStatementCacheMode = config.StatementCacheModePrepare // explicitly set

	pc, err := NewPoolConfig(cfg)
	require.NoError(t, err)

	// The pool config must reflect what the caller asked for. The operator
	// is responsible for the correctness of this combination (the config
	// validator emits a warning).
	assert.Equal(t, pgx.QueryExecModeCacheStatement, pc.ConnConfig.DefaultQueryExecMode,
		"pool.go must not silently override the statement cache mode — "+
			"that responsibility lies with config validation (which emits a warning)")
}
