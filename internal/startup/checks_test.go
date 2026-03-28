package startup

import (
	"context"
	"errors"
	"os"
	"testing"

	"stellarbill-backend/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock DB pinger ---

type mockPinger struct {
	err error
}

func (m *mockPinger) PingContext(ctx context.Context) error {
	return m.err
}

// --- helpers ---

// setRequiredEnv sets the minimum env vars for config.Validate() to pass.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb")
	t.Setenv("JWT_SECRET", "TestSecret123!xyz")
}

// stubMigrationStatus returns a MigrationStatusFunc with fixed values.
func stubMigrationStatus(applied, local int, err error) MigrationStatusFunc {
	return func(ctx context.Context) (int, int, error) {
		return applied, local, err
	}
}

// --- tests ---

func TestRunChecks_AllPass(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	db := &mockPinger{err: nil}
	migFn := stubMigrationStatus(4, 4, nil)

	results := RunChecks(cfg, db, migFn)

	require.Len(t, results, 3)
	for _, r := range results {
		assert.Equal(t, StatusPass, r.Status, "check %s should pass", r.Name)
	}
	assert.Equal(t, "ready", OverallStatus(results))
}

func TestRunChecks_DBDown(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	db := &mockPinger{err: errors.New("connection refused")}
	migFn := stubMigrationStatus(4, 4, nil)

	results := RunChecks(cfg, db, migFn)

	var dbCheck CheckResult
	for _, r := range results {
		if r.Name == "database" {
			dbCheck = r
		}
	}
	assert.Equal(t, StatusFail, dbCheck.Status)
	assert.Contains(t, dbCheck.Message, "connection refused")
	assert.True(t, HasFailures(results))
	assert.Equal(t, "unavailable", OverallStatus(results))
}

func TestRunChecks_NilDB(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	results := RunChecks(cfg, nil, stubMigrationStatus(0, 0, nil))

	var dbCheck CheckResult
	for _, r := range results {
		if r.Name == "database" {
			dbCheck = r
		}
	}
	assert.Equal(t, StatusFail, dbCheck.Status)
	assert.Contains(t, dbCheck.Message, "no database connection")
}

func TestRunChecks_PendingMigrations(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	db := &mockPinger{err: nil}
	migFn := stubMigrationStatus(2, 5, nil)

	results := RunChecks(cfg, db, migFn)

	var migCheck CheckResult
	for _, r := range results {
		if r.Name == "migrations" {
			migCheck = r
		}
	}
	assert.Equal(t, StatusWarn, migCheck.Status)
	assert.Contains(t, migCheck.Message, "3 pending")
	assert.Equal(t, "degraded", OverallStatus(results))
}

func TestRunChecks_MigrationQueryError(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	db := &mockPinger{err: nil}
	migFn := stubMigrationStatus(0, 0, errors.New("schema_migrations does not exist"))

	results := RunChecks(cfg, db, migFn)

	var migCheck CheckResult
	for _, r := range results {
		if r.Name == "migrations" {
			migCheck = r
		}
	}
	assert.Equal(t, StatusWarn, migCheck.Status)
	assert.Contains(t, migCheck.Message, "could not check")
}

func TestRunChecks_NilMigrationFunc(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := config.Load()
	require.NoError(t, err)

	db := &mockPinger{err: nil}

	results := RunChecks(cfg, db, nil)

	// Should only have config + database checks (no migrations check)
	assert.Len(t, results, 2)
	for _, r := range results {
		assert.NotEqual(t, "migrations", r.Name)
	}
}

func TestRunChecks_ConfigInvalid(t *testing.T) {
	// Unset required env vars to make config validation fail
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("JWT_SECRET")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "")

	// Load will fail, so we test with a zero-value config
	cfg := config.Config{Env: "test"}
	db := &mockPinger{err: nil}

	results := RunChecks(cfg, db, stubMigrationStatus(0, 0, nil))

	var cfgCheck CheckResult
	for _, r := range results {
		if r.Name == "config" {
			cfgCheck = r
		}
	}
	assert.Equal(t, StatusFail, cfgCheck.Status)
	assert.Contains(t, cfgCheck.Message, "validation failed")
}

func TestFormatResults(t *testing.T) {
	results := []CheckResult{
		{Name: "config", Status: StatusPass, Message: "loaded", DurationMs: 1},
		{Name: "database", Status: StatusFail, Message: "down", DurationMs: 50},
		{Name: "migrations", Status: StatusWarn, Message: "2 pending", DurationMs: 10},
	}

	output := FormatResults(results)
	assert.Contains(t, output, "[PASS]")
	assert.Contains(t, output, "[FAIL]")
	assert.Contains(t, output, "[WARN]")
	assert.Contains(t, output, "config")
	assert.Contains(t, output, "database")
	assert.Contains(t, output, "migrations")
}

func TestHasFailures(t *testing.T) {
	t.Run("no failures", func(t *testing.T) {
		results := []CheckResult{
			{Status: StatusPass},
			{Status: StatusWarn},
		}
		assert.False(t, HasFailures(results))
	})

	t.Run("with failure", func(t *testing.T) {
		results := []CheckResult{
			{Status: StatusPass},
			{Status: StatusFail},
		}
		assert.True(t, HasFailures(results))
	})

	t.Run("empty", func(t *testing.T) {
		assert.False(t, HasFailures(nil))
	})
}

func TestOverallStatus(t *testing.T) {
	tests := []struct {
		name     string
		statuses []Status
		want     string
	}{
		{"all pass", []Status{StatusPass, StatusPass}, "ready"},
		{"one warn", []Status{StatusPass, StatusWarn}, "degraded"},
		{"one fail", []Status{StatusPass, StatusFail}, "unavailable"},
		{"fail beats warn", []Status{StatusWarn, StatusFail}, "unavailable"},
		{"empty", nil, "ready"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var results []CheckResult
			for _, s := range tt.statuses {
				results = append(results, CheckResult{Status: s})
			}
			assert.Equal(t, tt.want, OverallStatus(results))
		})
	}
}
