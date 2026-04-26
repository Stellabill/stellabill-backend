package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validPoolEnv returns a minimal set of env vars that satisfy all required
// config fields so pool-specific tests can focus on pool vars only.
func validPoolEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   validTestSecret,
		"PORT":         "8080",
		"ENV":          "development",
	}
}

func TestDBPool_DefaultsApplied(t *testing.T) {
	withEnvVars(t, validPoolEnv(), func() {
		cfg, err := Load()
		require.NoError(t, err)

		assert.Equal(t, DefaultDBPoolMaxConns, cfg.DBPoolMaxConns)
		assert.Equal(t, DefaultDBPoolMinConns, cfg.DBPoolMinConns)
		assert.Equal(t, DefaultDBPoolMaxConnLifetime, cfg.DBPoolMaxConnLifetime)
		assert.Equal(t, DefaultDBPoolMaxConnIdleTime, cfg.DBPoolMaxConnIdleTime)
		assert.Equal(t, DefaultDBPoolConnectTimeout, cfg.DBPoolConnectTimeout)
		assert.Equal(t, DefaultDBPoolHealthCheckPeriod, cfg.DBPoolHealthCheckPeriod)
		assert.Equal(t, DefaultDBPoolMetricsInterval, cfg.DBPoolMetricsInterval)
	})
}

func TestDBPool_CustomValuesAccepted(t *testing.T) {
	env := validPoolEnv()
	env["DB_POOL_MAX_CONNS"] = "50"
	env["DB_POOL_MIN_CONNS"] = "5"
	env["DB_POOL_MAX_CONN_LIFETIME"] = "7200"
	env["DB_POOL_MAX_CONN_IDLE_TIME"] = "300"
	env["DB_POOL_CONNECT_TIMEOUT"] = "10"
	env["DB_POOL_HEALTH_CHECK_PERIOD"] = "60"
	env["DB_POOL_METRICS_INTERVAL"] = "30"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)

		assert.Equal(t, 50, cfg.DBPoolMaxConns)
		assert.Equal(t, 5, cfg.DBPoolMinConns)
		assert.Equal(t, 7200, cfg.DBPoolMaxConnLifetime)
		assert.Equal(t, 300, cfg.DBPoolMaxConnIdleTime)
		assert.Equal(t, 10, cfg.DBPoolConnectTimeout)
		assert.Equal(t, 60, cfg.DBPoolHealthCheckPeriod)
		assert.Equal(t, 30, cfg.DBPoolMetricsInterval)
	})
}

func TestDBPool_InvalidMaxConns_FallsBackToDefault(t *testing.T) {
	env := validPoolEnv()
	env["DB_POOL_MAX_CONNS"] = "not-a-number"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultDBPoolMaxConns, cfg.DBPoolMaxConns, "invalid value should fall back to default")
	})
}

func TestDBPool_MaxConnsZero_FallsBackToDefault(t *testing.T) {
	env := validPoolEnv()
	env["DB_POOL_MAX_CONNS"] = "0" // below MinDBPoolMaxConns=1

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultDBPoolMaxConns, cfg.DBPoolMaxConns)
	})
}

func TestDBPool_MaxConnsAboveCeiling_FallsBackToDefault(t *testing.T) {
	env := validPoolEnv()
	env["DB_POOL_MAX_CONNS"] = "9999" // above MaxDBPoolMaxConns=500

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultDBPoolMaxConns, cfg.DBPoolMaxConns)
	})
}

func TestDBPool_MinConnsExceedsMax_ClampedWithWarning(t *testing.T) {
	env := validPoolEnv()
	env["DB_POOL_MAX_CONNS"] = "10"
	env["DB_POOL_MIN_CONNS"] = "20" // intentionally > max

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		// MinConns must be clamped to MaxConns
		assert.Equal(t, cfg.DBPoolMaxConns, cfg.DBPoolMinConns,
			"MinConns should be clamped to MaxConns")

		// A warning must be emitted
		vr := cfg.Validate()
		hasWarning := false
		for _, w := range vr.Warnings {
			if len(w) > 0 {
				hasWarning = true
				break
			}
		}
		assert.True(t, hasWarning, "expected at least one warning for min > max")
	})
}

func TestDBPool_ConnectTimeoutBelowMin_FallsBackToDefault(t *testing.T) {
	env := validPoolEnv()
	env["DB_POOL_CONNECT_TIMEOUT"] = "0" // below MinDBPoolTimeout=1

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultDBPoolConnectTimeout, cfg.DBPoolConnectTimeout)
	})
}

func TestDBPool_ConnectTimeoutAboveMax_FallsBackToDefault(t *testing.T) {
	env := validPoolEnv()
	env["DB_POOL_CONNECT_TIMEOUT"] = "999" // above MaxDBPoolTimeout=300

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultDBPoolConnectTimeout, cfg.DBPoolConnectTimeout)
	})
}

func TestDBPool_IdleTimeGteLifetime_ProducesWarning(t *testing.T) {
	env := validPoolEnv()
	env["DB_POOL_MAX_CONN_LIFETIME"] = "600"
	env["DB_POOL_MAX_CONN_IDLE_TIME"] = "600" // equal to lifetime

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)

		vr := cfg.Validate()
		found := false
		for _, w := range vr.Warnings {
			if len(w) > 0 {
				found = true
				break
			}
		}
		assert.True(t, found, "expected warning when idle_time >= lifetime")
	})
}

func TestDBPool_MaxConnsOne_IsValid(t *testing.T) {
	env := validPoolEnv()
	env["DB_POOL_MAX_CONNS"] = "1"
	env["DB_POOL_MIN_CONNS"] = "0"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 1, cfg.DBPoolMaxConns)
		assert.Equal(t, 0, cfg.DBPoolMinConns)
	})
}

func TestDBPool_MaxConns500_IsValid(t *testing.T) {
	env := validPoolEnv()
	env["DB_POOL_MAX_CONNS"] = "500"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 500, cfg.DBPoolMaxConns)
	})
}

func TestDBPool_MetricsInterval_InvalidFallsBack(t *testing.T) {
	env := validPoolEnv()
	env["DB_POOL_METRICS_INTERVAL"] = "-5"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultDBPoolMetricsInterval, cfg.DBPoolMetricsInterval)
	})
}
