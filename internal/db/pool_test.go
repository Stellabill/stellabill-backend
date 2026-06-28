package db

import (
	"context"
	"testing"
	"time"

	"stellarbill-backend/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseCfg returns a Config with a valid connection string and explicit DB pool
// tuning values, mirroring what config.Load produces after validation.
func baseCfg() config.Config {
	return config.Config{
		DBConn:                  "postgres://user:pass@localhost:5432/app?sslmode=disable",
		DBPoolMaxConns:          17,
		DBPoolMinConns:          3,
		DBPoolMaxConnLifetime:   1800,
		DBPoolMaxConnIdleTime:   300,
		DBPoolConnectTimeout:    7,
		DBPoolHealthCheckPeriod: 45,
		DBPoolMetricsInterval:   15,
	}
}

func TestNewPoolConfig_AppliesTuningFields(t *testing.T) {
	cfg := baseCfg()

	pc, err := NewPoolConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, pc)

	assert.Equal(t, int32(17), pc.MaxConns)
	assert.Equal(t, int32(3), pc.MinConns)
	assert.Equal(t, 1800*time.Second, pc.MaxConnLifetime)
	assert.Equal(t, 300*time.Second, pc.MaxConnIdleTime)
	assert.Equal(t, 45*time.Second, pc.HealthCheckPeriod)

	require.NotNil(t, pc.ConnConfig)
	assert.Equal(t, 7*time.Second, pc.ConnConfig.ConnectTimeout,
		"DBPoolConnectTimeout must map onto the per-dial ConnectTimeout")
}

func TestNewPoolConfig_EmptyDBConn(t *testing.T) {
	cfg := baseCfg()
	cfg.DBConn = ""

	pc, err := NewPoolConfig(cfg)
	assert.Error(t, err)
	assert.Nil(t, pc)
}

func TestNewPoolConfig_InvalidDBConn(t *testing.T) {
	cfg := baseCfg()
	cfg.DBConn = "://not-a-valid-dsn"

	pc, err := NewPoolConfig(cfg)
	assert.Error(t, err)
	assert.Nil(t, pc)
}

func TestNewPool_EmptyDBConnReturnsNilNil(t *testing.T) {
	cfg := baseCfg()
	cfg.DBConn = ""

	pool, err := NewPool(context.Background(), cfg)
	assert.NoError(t, err, "empty DATABASE_URL must degrade gracefully, not error")
	assert.Nil(t, pool, "no pool should be created without a connection string")
}

// TestNewPool_ConnectTimeout exercises the connect-timeout path: a non-routable
// address must cause NewPool to fail fast (via the startup Ping) rather than
// hang, and it must not leak an open pool.
func TestNewPool_ConnectTimeout(t *testing.T) {
	cfg := baseCfg()
	// RFC 5737 TEST-NET-1 address — guaranteed non-routable, so the dial blocks
	// until the timeout fires.
	cfg.DBConn = "postgres://user:pass@192.0.2.1:5432/app?sslmode=disable&connect_timeout=1"
	cfg.DBPoolConnectTimeout = 1

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	pool, err := NewPool(ctx, cfg)
	elapsed := time.Since(start)

	require.Error(t, err, "unreachable host must surface an error")
	assert.Nil(t, pool, "failed pool must be closed and returned as nil")
	assert.Less(t, elapsed, 5*time.Second, "must fail fast via connect timeout, not hang")
}

func TestPoolPinger_NilPool(t *testing.T) {
	var p *PoolPinger
	err := p.PingContext(context.Background())
	assert.Error(t, err, "nil PoolPinger must report an error, not panic")

	p2 := &PoolPinger{Pool: nil}
	err = p2.PingContext(context.Background())
	assert.Error(t, err, "PoolPinger wrapping a nil pool must report an error")
}

// TestPoolPinger_SatisfiesDBPinger is a compile-time assertion that *PoolPinger
// implements the PingContext method shape required by handlers.DBPinger. We
// declare the interface locally to avoid importing handlers (which would create
// an import cycle and currently fails to compile for unrelated reasons).
func TestPoolPinger_SatisfiesDBPinger(t *testing.T) {
	type dbPinger interface {
		PingContext(ctx context.Context) error
	}
	var _ dbPinger = (*PoolPinger)(nil)
}
