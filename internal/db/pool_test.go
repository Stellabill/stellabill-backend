package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"stellarbill-backend/internal/config"
)

// baseConfig returns a minimal valid Config with all pool fields set to
// their defaults so individual tests only need to override what they care about.
func baseConfig() config.Config {
	return config.Config{
		DBConn:                  "postgres://user:pass@localhost:5432/testdb?sslmode=disable",
		DBPoolMaxConns:          config.DefaultDBPoolMaxConns,
		DBPoolMinConns:          config.DefaultDBPoolMinConns,
		DBPoolMaxConnLifetime:   config.DefaultDBPoolMaxConnLifetime,
		DBPoolMaxConnIdleTime:   config.DefaultDBPoolMaxConnIdleTime,
		DBPoolConnectTimeout:    config.DefaultDBPoolConnectTimeout,
		DBPoolHealthCheckPeriod: config.DefaultDBPoolHealthCheckPeriod,
		DBPoolMetricsInterval:   config.DefaultDBPoolMetricsInterval,
	}
}

// TestBuildPoolConfig_Defaults verifies that the default config values are
// correctly translated into pgxpool.Config fields.
func TestBuildPoolConfig_Defaults(t *testing.T) {
	cfg := baseConfig()
	poolCfg, err := buildPoolConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, int32(config.DefaultDBPoolMaxConns), poolCfg.MaxConns)
	assert.Equal(t, int32(config.DefaultDBPoolMinConns), poolCfg.MinConns)
	assert.Equal(t,
		time.Duration(config.DefaultDBPoolMaxConnLifetime)*time.Second,
		poolCfg.MaxConnLifetime,
	)
	assert.Equal(t,
		time.Duration(config.DefaultDBPoolMaxConnIdleTime)*time.Second,
		poolCfg.MaxConnIdleTime,
	)
	assert.Equal(t,
		time.Duration(config.DefaultDBPoolConnectTimeout)*time.Second,
		poolCfg.ConnConfig.ConnectTimeout,
	)
	assert.Equal(t,
		time.Duration(config.DefaultDBPoolHealthCheckPeriod)*time.Second,
		poolCfg.HealthCheckPeriod,
	)
}

// TestBuildPoolConfig_CustomValues verifies that non-default values are
// applied correctly.
func TestBuildPoolConfig_CustomValues(t *testing.T) {
	cfg := baseConfig()
	cfg.DBPoolMaxConns = 50
	cfg.DBPoolMinConns = 5
	cfg.DBPoolMaxConnLifetime = 7200
	cfg.DBPoolMaxConnIdleTime = 300
	cfg.DBPoolConnectTimeout = 10
	cfg.DBPoolHealthCheckPeriod = 60

	poolCfg, err := buildPoolConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, int32(50), poolCfg.MaxConns)
	assert.Equal(t, int32(5), poolCfg.MinConns)
	assert.Equal(t, 7200*time.Second, poolCfg.MaxConnLifetime)
	assert.Equal(t, 300*time.Second, poolCfg.MaxConnIdleTime)
	assert.Equal(t, 10*time.Second, poolCfg.ConnConfig.ConnectTimeout)
	assert.Equal(t, 60*time.Second, poolCfg.HealthCheckPeriod)
}

// TestBuildPoolConfig_JitterIsOnetenth verifies that the lifetime jitter is
// set to 10 % of MaxConnLifetime to spread reconnects.
func TestBuildPoolConfig_JitterIsOnetenth(t *testing.T) {
	cfg := baseConfig()
	cfg.DBPoolMaxConnLifetime = 1000

	poolCfg, err := buildPoolConfig(cfg)
	require.NoError(t, err)

	expected := time.Duration(1000) * time.Second / 10
	assert.Equal(t, expected, poolCfg.MaxConnLifetimeJitter)
}

// TestBuildPoolConfig_InvalidDSN verifies that a malformed DSN returns an error.
func TestBuildPoolConfig_InvalidDSN(t *testing.T) {
	cfg := baseConfig()
	cfg.DBConn = "://not-a-valid-dsn"

	_, err := buildPoolConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse DSN")
}

// TestBuildPoolConfig_MinConnsRespected verifies that MinConns=0 is valid
// (pool starts empty and grows on demand).
func TestBuildPoolConfig_MinConnsRespected(t *testing.T) {
	cfg := baseConfig()
	cfg.DBPoolMinConns = 0

	poolCfg, err := buildPoolConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, int32(0), poolCfg.MinConns)
}

// TestBuildPoolConfig_MaxConnsOne verifies the minimum useful pool size.
func TestBuildPoolConfig_MaxConnsOne(t *testing.T) {
	cfg := baseConfig()
	cfg.DBPoolMaxConns = 1
	cfg.DBPoolMinConns = 0

	poolCfg, err := buildPoolConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, int32(1), poolCfg.MaxConns)
}

// TestBuildPoolConfig_LargePool verifies a large pool ceiling is accepted.
func TestBuildPoolConfig_LargePool(t *testing.T) {
	cfg := baseConfig()
	cfg.DBPoolMaxConns = 500

	poolCfg, err := buildPoolConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, int32(500), poolCfg.MaxConns)
}

// TestBuildPoolConfig_ShortConnectTimeout verifies a 1-second timeout is
// accepted (minimum allowed by config validation).
func TestBuildPoolConfig_ShortConnectTimeout(t *testing.T) {
	cfg := baseConfig()
	cfg.DBPoolConnectTimeout = 1

	poolCfg, err := buildPoolConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, 1*time.Second, poolCfg.ConnConfig.ConnectTimeout)
}

// TestBuildPoolConfig_DSNPreserved verifies that the DSN host/db are not
// mutated by buildPoolConfig.
func TestBuildPoolConfig_DSNPreserved(t *testing.T) {
	cfg := baseConfig()
	cfg.DBConn = "postgres://alice:secret@db.example.com:5432/billing?sslmode=require"

	poolCfg, err := buildPoolConfig(cfg)
	require.NoError(t, err)

	assert.Equal(t, "db.example.com", poolCfg.ConnConfig.Host)
	assert.Equal(t, "billing", poolCfg.ConnConfig.Database)
	assert.Equal(t, "alice", poolCfg.ConnConfig.User)
}
