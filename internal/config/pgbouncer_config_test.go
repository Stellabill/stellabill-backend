package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validPgBouncerEnv returns a minimal env that satisfies required config fields,
// extending validPoolEnv so both pool and PgBouncer tests can share it.
func validPgBouncerEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   "Test1!JwtSecret-MixedAlphaNumeric@123",
		"ADMIN_TOKEN":  "Admin1!Token-MixedAlphaNumeric@123",
		"REDIS_URL":    "redis://localhost:6379",
		"PORT":         "8080",
		"ENV":          "development",
	}
}

// ─── PGBOUNCER_ENABLED ────────────────────────────────────────────────────────

func TestPgBouncer_EnabledDefaultsFalse(t *testing.T) {
	withEnvVars(t, validPgBouncerEnv(), func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.False(t, cfg.PgBouncerEnabled, "PgBouncer should be disabled by default")
	})
}

func TestPgBouncer_EnabledTrue(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_ENABLED"] = "true"
	env["DB_STATEMENT_CACHE_MODE"] = "describe" // avoid the warning-only path

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.True(t, cfg.PgBouncerEnabled)
	})
}

func TestPgBouncer_EnabledInvalidValue_UsesDefault(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_ENABLED"] = "yes-please" // invalid bool

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.False(t, cfg.PgBouncerEnabled, "invalid bool should fall back to default false")

		vr := cfg.Validate()
		found := false
		for _, w := range vr.Warnings {
			if strings.Contains(w, "PGBOUNCER_ENABLED") {
				found = true
				break
			}
		}
		assert.True(t, found, "expected a warning for invalid PGBOUNCER_ENABLED value")
	})
}

// ─── PGBOUNCER_HOST ───────────────────────────────────────────────────────────

func TestPgBouncer_HostDefault(t *testing.T) {
	withEnvVars(t, validPgBouncerEnv(), func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultPgBouncerHost, cfg.PgBouncerHost)
	})
}

func TestPgBouncer_HostCustom(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_HOST"] = "10.0.0.1"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "10.0.0.1", cfg.PgBouncerHost)
	})
}

// ─── PGBOUNCER_PORT ───────────────────────────────────────────────────────────

func TestPgBouncer_PortDefault(t *testing.T) {
	withEnvVars(t, validPgBouncerEnv(), func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultPgBouncerPort, cfg.PgBouncerPort)
	})
}

func TestPgBouncer_PortCustom(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_PORT"] = "6432"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 6432, cfg.PgBouncerPort)
	})
}

func TestPgBouncer_PortInvalid_FallsBackToDefault(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_PORT"] = "not-a-port"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultPgBouncerPort, cfg.PgBouncerPort)

		vr := cfg.Validate()
		found := false
		for _, w := range vr.Warnings {
			if strings.Contains(w, "PGBOUNCER_PORT") {
				found = true
				break
			}
		}
		assert.True(t, found, "expected a warning for invalid PGBOUNCER_PORT")
	})
}

func TestPgBouncer_PortZero_FallsBackToDefault(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_PORT"] = "0" // below MinPgBouncerPort

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultPgBouncerPort, cfg.PgBouncerPort)
	})
}

func TestPgBouncer_PortAboveCeiling_FallsBackToDefault(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_PORT"] = "99999"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultPgBouncerPort, cfg.PgBouncerPort)
	})
}

// ─── DB_STATEMENT_CACHE_MODE ──────────────────────────────────────────────────

func TestPgBouncer_StatementCacheModeDefault(t *testing.T) {
	withEnvVars(t, validPgBouncerEnv(), func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultDBStatementCacheMode, cfg.DBStatementCacheMode,
			"default statement cache mode should be 'prepare'")
	})
}

func TestPgBouncer_StatementCacheModeDescribe(t *testing.T) {
	env := validPgBouncerEnv()
	env["DB_STATEMENT_CACHE_MODE"] = "describe"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, StatementCacheModeDescribe, cfg.DBStatementCacheMode)
	})
}

func TestPgBouncer_StatementCacheModeSimple(t *testing.T) {
	env := validPgBouncerEnv()
	env["DB_STATEMENT_CACHE_MODE"] = "simple"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, StatementCacheModeSimple, cfg.DBStatementCacheMode)
	})
}

func TestPgBouncer_StatementCacheModePrepare(t *testing.T) {
	env := validPgBouncerEnv()
	env["DB_STATEMENT_CACHE_MODE"] = "prepare"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, StatementCacheModePrepare, cfg.DBStatementCacheMode)
	})
}

func TestPgBouncer_StatementCacheModeInvalid_FallsBackToDefault(t *testing.T) {
	env := validPgBouncerEnv()
	env["DB_STATEMENT_CACHE_MODE"] = "turbo-cache"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultDBStatementCacheMode, cfg.DBStatementCacheMode,
			"invalid mode should fall back to default")

		vr := cfg.Validate()
		found := false
		for _, w := range vr.Warnings {
			if strings.Contains(w, "DB_STATEMENT_CACHE_MODE") {
				found = true
				break
			}
		}
		assert.True(t, found, "expected warning for invalid DB_STATEMENT_CACHE_MODE")
	})
}

// ─── PGBOUNCER_IDLE_IN_TX_TIMEOUT ────────────────────────────────────────────

func TestPgBouncer_IdleInTxTimeoutDefault(t *testing.T) {
	withEnvVars(t, validPgBouncerEnv(), func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultPgBouncerIdleInTxTimeout, cfg.PgBouncerIdleInTxTimeout)
	})
}

func TestPgBouncer_IdleInTxTimeoutCustom(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_IDLE_IN_TX_TIMEOUT"] = "60"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 60, cfg.PgBouncerIdleInTxTimeout)
	})
}

func TestPgBouncer_IdleInTxTimeoutZero_FallsBackToDefault(t *testing.T) {
	// A zero timeout would disable idle-in-transaction enforcement, which is
	// dangerous in production.  The validator rejects it and warns.
	env := validPgBouncerEnv()
	env["PGBOUNCER_IDLE_IN_TX_TIMEOUT"] = "0"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultPgBouncerIdleInTxTimeout, cfg.PgBouncerIdleInTxTimeout,
			"zero idle-in-transaction timeout should fall back to default")
	})
}

func TestPgBouncer_IdleInTxTimeoutAboveCeiling_FallsBackToDefault(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_IDLE_IN_TX_TIMEOUT"] = "999999"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultPgBouncerIdleInTxTimeout, cfg.PgBouncerIdleInTxTimeout)
	})
}

func TestPgBouncer_IdleInTxTimeoutInvalid_FallsBackToDefault(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_IDLE_IN_TX_TIMEOUT"] = "thirty"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, DefaultPgBouncerIdleInTxTimeout, cfg.PgBouncerIdleInTxTimeout)
	})
}

// ─── Cross-field: PgBouncer enabled + wrong statement cache mode ──────────────

func TestPgBouncer_EnabledWithPrepareMode_ProducesWarning(t *testing.T) {
	// This is the most important safety check: enabling PgBouncer transaction
	// pooling while keeping the default "prepare" statement cache mode will
	// cause "prepared statement does not exist" errors at runtime.
	env := validPgBouncerEnv()
	env["PGBOUNCER_ENABLED"] = "true"
	// DB_STATEMENT_CACHE_MODE not set → defaults to "prepare"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.True(t, cfg.PgBouncerEnabled)
		assert.Equal(t, StatementCacheModePrepare, cfg.DBStatementCacheMode)

		vr := cfg.Validate()
		found := false
		for _, w := range vr.Warnings {
			if strings.Contains(w, "prepare") && strings.Contains(w, "transaction pooling") {
				found = true
				break
			}
		}
		assert.True(t, found, "expected a warning about prepare mode with PgBouncer transaction pooling")
	})
}

func TestPgBouncer_EnabledWithDescribeMode_NoWarning(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_ENABLED"] = "true"
	env["DB_STATEMENT_CACHE_MODE"] = "describe"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.True(t, cfg.PgBouncerEnabled)
		assert.Equal(t, StatementCacheModeDescribe, cfg.DBStatementCacheMode)

		vr := cfg.Validate()
		for _, w := range vr.Warnings {
			assert.NotContains(t, w, "transaction pooling",
				"no transaction-pooling warning expected when describe mode is set")
		}
	})
}

func TestPgBouncer_EnabledWithSimpleMode_NoWarning(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_ENABLED"] = "true"
	env["DB_STATEMENT_CACHE_MODE"] = "simple"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)
		assert.True(t, cfg.PgBouncerEnabled)
		assert.Equal(t, StatementCacheModeSimple, cfg.DBStatementCacheMode)

		vr := cfg.Validate()
		for _, w := range vr.Warnings {
			assert.NotContains(t, w, "transaction pooling",
				"no transaction-pooling warning expected when simple mode is set")
		}
	})
}

// ─── Full happy-path PgBouncer config ─────────────────────────────────────────

func TestPgBouncer_FullConfiguration(t *testing.T) {
	env := validPgBouncerEnv()
	env["PGBOUNCER_ENABLED"] = "true"
	env["PGBOUNCER_HOST"] = "127.0.0.1"
	env["PGBOUNCER_PORT"] = "6432"
	env["DB_STATEMENT_CACHE_MODE"] = "describe"
	env["PGBOUNCER_IDLE_IN_TX_TIMEOUT"] = "45"

	withEnvVars(t, env, func() {
		cfg, err := Load()
		require.NoError(t, err)

		assert.True(t, cfg.PgBouncerEnabled)
		assert.Equal(t, "127.0.0.1", cfg.PgBouncerHost)
		assert.Equal(t, 6432, cfg.PgBouncerPort)
		assert.Equal(t, StatementCacheModeDescribe, cfg.DBStatementCacheMode)
		assert.Equal(t, 45, cfg.PgBouncerIdleInTxTimeout)
	})
}
