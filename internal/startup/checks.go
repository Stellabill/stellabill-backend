package startup

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/migrations"
)

// Status represents the result of a single startup check.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusWarn Status = "warn"
)

// CheckResult holds the outcome of one startup check.
type CheckResult struct {
	Name       string `json:"name"`
	Status     Status `json:"status"`
	Message    string `json:"message"`
	DurationMs int64  `json:"duration_ms"`
}

// DiagnosticsResponse is the machine-readable diagnostics payload.
type DiagnosticsResponse struct {
	Status        string        `json:"status"`
	Timestamp     string        `json:"timestamp"`
	UptimeSeconds float64       `json:"uptime_seconds"`
	Checks        []CheckResult `json:"checks"`
}

// DBPinger abstracts database connectivity checks.
type DBPinger interface {
	PingContext(ctx context.Context) error
}

// MigrationStatusFunc returns the count of applied and local migrations.
// This allows callers to inject the real implementation or a test stub.
type MigrationStatusFunc func(ctx context.Context) (applied int, local int, err error)

// NewMigrationStatusFunc builds a MigrationStatusFunc from a real DB and migration directory.
func NewMigrationStatusFunc(db *sql.DB, migrationsDir string) MigrationStatusFunc {
	return func(ctx context.Context) (int, int, error) {
		localMigs, err := migrations.LoadDir(migrationsDir)
		if err != nil {
			return 0, 0, fmt.Errorf("load migrations: %w", err)
		}
		runner := migrations.Runner{DB: db}
		appliedMigs, err := runner.Applied(ctx)
		if err != nil {
			return 0, len(localMigs), fmt.Errorf("query applied migrations: %w", err)
		}
		return len(appliedMigs), len(localMigs), nil
	}
}

// RunChecks executes all startup checks and returns the results.
// It validates config, database connectivity, and migration status.
func RunChecks(cfg config.Config, db DBPinger, migStatus MigrationStatusFunc) []CheckResult {
	var results []CheckResult

	results = append(results, checkConfig(cfg))
	results = append(results, checkDB(db))
	if migStatus != nil {
		results = append(results, checkMigrations(migStatus))
	}

	return results
}

// HasFailures returns true if any check has Status == StatusFail.
func HasFailures(results []CheckResult) bool {
	for _, r := range results {
		if r.Status == StatusFail {
			return true
		}
	}
	return false
}

// FormatResults returns a human-readable summary of check results.
func FormatResults(results []CheckResult) string {
	var b strings.Builder
	for _, r := range results {
		tag := "PASS"
		switch r.Status {
		case StatusFail:
			tag = "FAIL"
		case StatusWarn:
			tag = "WARN"
		}
		fmt.Fprintf(&b, "[%s] %-14s — %s (%dms)\n", tag, r.Name, r.Message, r.DurationMs)
	}
	return b.String()
}

// OverallStatus returns "ready" if all checks pass, "degraded" if there are
// only warnings, or "unavailable" if any check failed.
func OverallStatus(results []CheckResult) string {
	hasWarn := false
	for _, r := range results {
		if r.Status == StatusFail {
			return "unavailable"
		}
		if r.Status == StatusWarn {
			hasWarn = true
		}
	}
	if hasWarn {
		return "degraded"
	}
	return "ready"
}

func checkConfig(cfg config.Config) CheckResult {
	start := time.Now()
	vResult := cfg.Validate()

	dur := time.Since(start).Milliseconds()

	if !vResult.Valid() {
		return CheckResult{
			Name:       "config",
			Status:     StatusFail,
			Message:    fmt.Sprintf("validation failed: %s", vResult.Error()),
			DurationMs: dur,
		}
	}

	if len(vResult.Warnings) > 0 {
		return CheckResult{
			Name:       "config",
			Status:     StatusWarn,
			Message:    fmt.Sprintf("loaded with %d warning(s)", len(vResult.Warnings)),
			DurationMs: dur,
		}
	}

	return CheckResult{
		Name:       "config",
		Status:     StatusPass,
		Message:    "loaded and validated",
		DurationMs: dur,
	}
}

func checkDB(db DBPinger) CheckResult {
	start := time.Now()

	if db == nil {
		return CheckResult{
			Name:       "database",
			Status:     StatusFail,
			Message:    "no database connection provided",
			DurationMs: time.Since(start).Milliseconds(),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		msg := "connection failed"
		if ctx.Err() == context.DeadlineExceeded {
			msg = "connection timed out (5s)"
		}
		return CheckResult{
			Name:       "database",
			Status:     StatusFail,
			Message:    fmt.Sprintf("%s: %v", msg, err),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}

	return CheckResult{
		Name:       "database",
		Status:     StatusPass,
		Message:    "connected (ping OK)",
		DurationMs: time.Since(start).Milliseconds(),
	}
}

func checkMigrations(migStatus MigrationStatusFunc) CheckResult {
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	applied, local, err := migStatus(ctx)
	dur := time.Since(start).Milliseconds()

	if err != nil {
		return CheckResult{
			Name:       "migrations",
			Status:     StatusWarn,
			Message:    fmt.Sprintf("could not check: %v", err),
			DurationMs: dur,
		}
	}

	pending := local - applied
	if pending < 0 {
		pending = 0
	}

	if pending > 0 {
		return CheckResult{
			Name:       "migrations",
			Status:     StatusWarn,
			Message:    fmt.Sprintf("%d applied, %d pending", applied, pending),
			DurationMs: dur,
		}
	}

	return CheckResult{
		Name:       "migrations",
		Status:     StatusPass,
		Message:    fmt.Sprintf("%d applied, 0 pending", applied),
		DurationMs: dur,
	}
}
