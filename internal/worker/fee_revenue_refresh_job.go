package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// feeRevenueLogger is the minimal logging surface the refresh job needs. It is
// defined locally (rather than depending on a shared logger type) so the job is
// self-contained and easy to satisfy from tests with a no-op. A nil logger is
// also accepted by the job and simply disables logging.
type feeRevenueLogger interface {
	Error(msg string, keysAndValues ...any)
}

// Name of the materialized view and its freshness-metadata table. Kept in one
// place so the migration, worker, and service all agree on the identifiers.
const (
	feeRevenueViewName    = "mv_fee_revenue_monthly"
	feeRevenueStateTable  = "mv_fee_revenue_refresh_state"
	feeRevenueStateRowKey = true
)

// FeeRevenueRefreshConfig configures the materialized-view refresh job.
type FeeRevenueRefreshConfig struct {
	// PollInterval: how often the view is refreshed (default: 1h).
	PollInterval time.Duration
	// RefreshTimeout: context timeout for a single REFRESH (default: 5m).
	RefreshTimeout time.Duration
	// ShutdownTimeout: max time to wait for in-flight work on Stop() (default: 30s).
	ShutdownTimeout time.Duration
	// StalenessThreshold: how old last_refreshed_at may be before the data is
	// considered stale. Used by IsStale for the report's stale-but-served
	// signal (default: 2x PollInterval).
	StalenessThreshold time.Duration
}

// DefaultFeeRevenueRefreshConfig returns production-safe defaults: an hourly
// refresh as required by the report freshness SLA.
func DefaultFeeRevenueRefreshConfig() FeeRevenueRefreshConfig {
	return FeeRevenueRefreshConfig{
		PollInterval:       1 * time.Hour,
		RefreshTimeout:     5 * time.Minute,
		ShutdownTimeout:    30 * time.Second,
		StalenessThreshold: 2 * time.Hour,
	}
}

// withDefaults fills any zero-valued fields with their defaults so callers can
// override only what they care about.
func (c FeeRevenueRefreshConfig) withDefaults() FeeRevenueRefreshConfig {
	d := DefaultFeeRevenueRefreshConfig()
	if c.PollInterval <= 0 {
		c.PollInterval = d.PollInterval
	}
	if c.RefreshTimeout <= 0 {
		c.RefreshTimeout = d.RefreshTimeout
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = d.ShutdownTimeout
	}
	if c.StalenessThreshold <= 0 {
		// Default to twice the (possibly overridden) poll interval so a single
		// missed refresh does not immediately flag the data as stale.
		c.StalenessThreshold = 2 * c.PollInterval
	}
	return c
}

// feeRevenueStore abstracts the database operations the refresh job needs. The
// concrete implementation wraps *sql.DB; tests provide a fake so the
// orchestration, freshness recording, and stale-but-served logic can be
// exercised without Postgres (SQLite has no materialized views).
type feeRevenueStore interface {
	// Refresh refreshes the materialized view. concurrently selects
	// REFRESH ... CONCURRENTLY (non-blocking for readers) when true.
	Refresh(ctx context.Context, concurrently bool) error
	// MarkRefreshed records the moment the view was last refreshed.
	MarkRefreshed(ctx context.Context, at time.Time) error
	// LastRefreshedAt returns the recorded refresh time. ok is false when the
	// view has never been refreshed.
	LastRefreshedAt(ctx context.Context) (at time.Time, ok bool, err error)
}

// FeeRevenueRefreshJob periodically refreshes mv_fee_revenue_monthly and records
// its freshness so the admin fee report can be served from the aggregate.
//
// Refreshes use REFRESH MATERIALIZED VIEW CONCURRENTLY so in-flight report reads
// are never blocked. CONCURRENTLY cannot run against a view that has never held
// data (Postgres requires at least one prior non-concurrent populate), so the
// first successful refresh after startup falls back to a blocking refresh once;
// every subsequent refresh is concurrent.
type FeeRevenueRefreshJob struct {
	store  feeRevenueStore
	config FeeRevenueRefreshConfig
	logger feeRevenueLogger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	running atomic.Int32

	// populated tracks whether the view has held data at least once, so we know
	// whether CONCURRENTLY is safe.
	populated atomic.Bool

	// stats
	mu              sync.RWMutex
	refreshCount    int64
	failedCount     int64
	lastRunTime     time.Time
	lastRunError    error
	consecutiveErrs int
}

// NewFeeRevenueRefreshJob constructs a refresh job backed by the given database.
func NewFeeRevenueRefreshJob(db *sql.DB, config FeeRevenueRefreshConfig, l feeRevenueLogger) *FeeRevenueRefreshJob {
	return newFeeRevenueRefreshJob(&sqlFeeRevenueStore{db: db}, config, l)
}

// newFeeRevenueRefreshJob is the store-injecting constructor used by tests.
func newFeeRevenueRefreshJob(store feeRevenueStore, config FeeRevenueRefreshConfig, l feeRevenueLogger) *FeeRevenueRefreshJob {
	return &FeeRevenueRefreshJob{
		store:  store,
		config: config.withDefaults(),
		logger: l,
	}
}

// Start begins the refresh loop. It is safe to call Start only once.
func (j *FeeRevenueRefreshJob) Start() {
	j.ctx, j.cancel = context.WithCancel(context.Background())
	j.running.Store(1)

	j.wg.Add(1)
	go j.refreshLoop()
}

// Stop signals the refresh loop to exit and waits up to ShutdownTimeout for
// in-flight work to drain.
func (j *FeeRevenueRefreshJob) Stop() error {
	if j.cancel == nil {
		return nil
	}
	j.cancel()

	done := make(chan struct{})
	go func() {
		j.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		j.running.Store(0)
		return nil
	case <-time.After(j.config.ShutdownTimeout):
		j.running.Store(0)
		return fmt.Errorf("fee revenue refresh job shutdown timed out after %v", j.config.ShutdownTimeout)
	}
}

// Health returns nil if the job is running and not stuck in a failure loop.
func (j *FeeRevenueRefreshJob) Health() error {
	if j.running.Load() != 1 {
		return errors.New("fee revenue refresh job is not running")
	}

	j.mu.RLock()
	consec := j.consecutiveErrs
	j.mu.RUnlock()

	if consec > 5 {
		return fmt.Errorf("fee revenue refresh job has %d consecutive errors", consec)
	}
	return nil
}

// FeeRevenueRefreshStats reports refresh job statistics.
type FeeRevenueRefreshStats struct {
	Refreshed      int64
	Failed         int64
	LastRunTime    time.Time
	LastRunError   string
	ConsecutiveErr int
}

// GetStats returns a snapshot of the job's statistics.
func (j *FeeRevenueRefreshJob) GetStats() FeeRevenueRefreshStats {
	j.mu.RLock()
	defer j.mu.RUnlock()

	errMsg := ""
	if j.lastRunError != nil {
		errMsg = j.lastRunError.Error()
	}
	return FeeRevenueRefreshStats{
		Refreshed:      j.refreshCount,
		Failed:         j.failedCount,
		LastRunTime:    j.lastRunTime,
		LastRunError:   errMsg,
		ConsecutiveErr: j.consecutiveErrs,
	}
}

// IsStale reports whether the view's data is older than StalenessThreshold as of
// `now`. never is true when the view has never been refreshed. Callers use this
// to serve stale-but-fresh-enough data while flagging it to the client.
func (j *FeeRevenueRefreshJob) IsStale(ctx context.Context, now time.Time) (stale bool, lastRefreshed time.Time, never bool, err error) {
	at, ok, err := j.store.LastRefreshedAt(ctx)
	if err != nil {
		return false, time.Time{}, false, err
	}
	if !ok {
		return true, time.Time{}, true, nil
	}
	return now.Sub(at) > j.config.StalenessThreshold, at, false, nil
}

// refreshLoop runs the main refresh loop.
func (j *FeeRevenueRefreshJob) refreshLoop() {
	defer j.wg.Done()

	ticker := time.NewTicker(j.config.PollInterval)
	defer ticker.Stop()

	// Refresh once immediately on startup so the view is populated promptly.
	j.refreshOnce()

	for {
		select {
		case <-j.ctx.Done():
			return
		case <-ticker.C:
			j.refreshOnce()
		}
	}
}

// refreshOnce performs a single refresh and records freshness on success.
func (j *FeeRevenueRefreshJob) refreshOnce() {
	ctx, cancel := context.WithTimeout(j.ctx, j.config.RefreshTimeout)
	defer cancel()

	if err := j.doRefresh(ctx); err != nil {
		j.recordError(err)
		return
	}

	now := time.Now().UTC()
	if err := j.store.MarkRefreshed(ctx, now); err != nil {
		// The view is fresh but we failed to persist the timestamp; surface it
		// so the report does not silently report stale data.
		j.recordError(fmt.Errorf("mark refreshed: %w", err))
		return
	}

	j.mu.Lock()
	j.refreshCount++
	j.lastRunTime = now
	j.lastRunError = nil
	j.consecutiveErrs = 0
	j.mu.Unlock()
}

// doRefresh refreshes the view, using CONCURRENTLY once the view has been
// populated at least once. The very first refresh must be non-concurrent
// because Postgres rejects CONCURRENTLY against a never-populated view.
func (j *FeeRevenueRefreshJob) doRefresh(ctx context.Context) error {
	concurrently := j.populated.Load()

	err := j.store.Refresh(ctx, concurrently)
	if err == nil {
		j.populated.Store(true)
		return nil
	}

	// Defensive fallback: if a concurrent refresh is rejected because the view
	// was never populated (e.g. another process recreated it), retry once
	// without CONCURRENTLY to recover automatically.
	if concurrently && isNotPopulatedErr(err) {
		if fbErr := j.store.Refresh(ctx, false); fbErr != nil {
			return fmt.Errorf("non-concurrent fallback refresh: %w", fbErr)
		}
		j.populated.Store(true)
		return nil
	}
	return err
}

// isNotPopulatedErr detects the Postgres error raised when REFRESH ...
// CONCURRENTLY targets a materialized view that has never been populated.
func isNotPopulatedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "has not been populated") ||
		strings.Contains(msg, "cannot refresh materialized view") && strings.Contains(msg, "concurrently")
}

func (j *FeeRevenueRefreshJob) recordError(err error) {
	j.mu.Lock()
	j.failedCount++
	j.lastRunError = err
	j.consecutiveErrs++
	j.mu.Unlock()

	if j.logger != nil {
		j.logger.Error("Fee revenue refresh job error", "error", err.Error())
	}
}

// sqlFeeRevenueStore is the production feeRevenueStore backed by *sql.DB.
type sqlFeeRevenueStore struct {
	db *sql.DB
}

func (s *sqlFeeRevenueStore) Refresh(ctx context.Context, concurrently bool) error {
	stmt := "REFRESH MATERIALIZED VIEW " + feeRevenueViewName
	if concurrently {
		stmt = "REFRESH MATERIALIZED VIEW CONCURRENTLY " + feeRevenueViewName
	}
	_, err := s.db.ExecContext(ctx, stmt)
	return err
}

func (s *sqlFeeRevenueStore) MarkRefreshed(ctx context.Context, at time.Time) error {
	// The singleton row is seeded by the migration; UPDATE keeps the CHECK
	// constraint and PK simple. Use UTC to keep comparisons stable.
	_, err := s.db.ExecContext(ctx,
		`UPDATE `+feeRevenueStateTable+` SET last_refreshed_at = $1 WHERE id = $2`,
		at.UTC(), feeRevenueStateRowKey,
	)
	return err
}

func (s *sqlFeeRevenueStore) LastRefreshedAt(ctx context.Context) (time.Time, bool, error) {
	var at sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT last_refreshed_at FROM `+feeRevenueStateTable+` WHERE id = $1`,
		feeRevenueStateRowKey,
	).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	if !at.Valid {
		return time.Time{}, false, nil
	}
	return at.Time.UTC(), true, nil
}
