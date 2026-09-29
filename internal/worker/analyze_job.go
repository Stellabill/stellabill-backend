package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"stellarbill-backend/internal/metrics"
	"stellarbill-backend/internal/timeutil"

	"github.com/jackc/pgx/v5/pgxpool"
)

// analyzeLogger is the minimal logging surface the analyze job needs.
type analyzeLogger interface {
	Error(msg string, keysAndValues ...any)
}

// AnalyzeConfig configures the periodic ANALYZE worker.
type AnalyzeConfig struct {
	// OutboxInterval: how often to ANALYZE outbox_events (default: 5m).
	OutboxInterval time.Duration
	// StatementsInterval: how often to ANALYZE statements (default: 15m).
	StatementsInterval time.Duration
	// SubscriptionsInterval: how often to ANALYZE subscriptions (default: 30m).
	SubscriptionsInterval time.Duration
	// AnalyzeTimeout: context timeout for a single ANALYZE (default: 30s).
	AnalyzeTimeout time.Duration
	// ShutdownTimeout: max time to wait for in-flight work on Stop() (default: 30s).
	ShutdownTimeout time.Duration
	// TickInterval: how often the loop checks which tables are due (default: 30s).
	TickInterval time.Duration
}

// hotTable holds the config for a single table tracked by the analyze job.
type hotTable struct {
	name     string
	interval time.Duration
}

// DefaultAnalyzeConfig returns production-safe defaults. Tuning rationale:
//   - outbox_events has the highest write volume and benefits from frequent stats.
//   - statements has moderate write volume (batch inserts from billing runs).
//   - subscriptions has low write volume (primarily status transitions).
func DefaultAnalyzeConfig() AnalyzeConfig {
	return AnalyzeConfig{
		OutboxInterval:        5 * time.Minute,
		StatementsInterval:    15 * time.Minute,
		SubscriptionsInterval: 30 * time.Minute,
		AnalyzeTimeout:        30 * time.Second,
		ShutdownTimeout:       30 * time.Second,
		TickInterval:          30 * time.Second,
	}
}

func (c AnalyzeConfig) withDefaults() AnalyzeConfig {
	d := DefaultAnalyzeConfig()
	if c.OutboxInterval <= 0 {
		c.OutboxInterval = d.OutboxInterval
	}
	if c.StatementsInterval <= 0 {
		c.StatementsInterval = d.StatementsInterval
	}
	if c.SubscriptionsInterval <= 0 {
		c.SubscriptionsInterval = d.SubscriptionsInterval
	}
	if c.AnalyzeTimeout <= 0 {
		c.AnalyzeTimeout = d.AnalyzeTimeout
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = d.ShutdownTimeout
	}
	if c.TickInterval <= 0 {
		c.TickInterval = d.TickInterval
	}
	return c
}

func (c AnalyzeConfig) tables() []hotTable {
	return []hotTable{
		{name: "outbox_events", interval: c.OutboxInterval},
		{name: "statements", interval: c.StatementsInterval},
		{name: "subscriptions", interval: c.SubscriptionsInterval},
	}
}

// analyzeStore abstracts the ANALYZE operation for testability.
type analyzeStore interface {
	Analyze(ctx context.Context, table string) error
}

// AnalyzeJob periodically runs ANALYZE on hot tables to prevent plan
// degradation from stale statistics. Each table is analyzed on its own
// cadence tuned to its write volume.
type AnalyzeJob struct {
	store  analyzeStore
	config AnalyzeConfig
	logger analyzeLogger
	clock  timeutil.Clock

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	running atomic.Int32

	// per-table tracking
	mu              sync.RWMutex
	analyzeCount    map[string]int64
	failedCount     map[string]int64
	lastRunTime     map[string]time.Time
	lastRunError    map[string]error
	consecutiveErrs map[string]int
}

// NewAnalyzeJob constructs an AnalyzeJob backed by the given database pool.
func NewAnalyzeJob(pool *pgxpool.Pool, config AnalyzeConfig, l analyzeLogger) *AnalyzeJob {
	return newAnalyzeJob(&pgxAnalyzeStore{pool: pool}, config, l)
}

// newAnalyzeJob is the store-injecting constructor used by tests.
func newAnalyzeJob(store analyzeStore, config AnalyzeConfig, l analyzeLogger) *AnalyzeJob {
	cfg := config.withDefaults()
	tables := cfg.tables()
	return &AnalyzeJob{
		store:           store,
		config:          cfg,
		logger:          l,
		clock:           timeutil.SystemClock,
		analyzeCount:    make(map[string]int64, len(tables)),
		failedCount:     make(map[string]int64, len(tables)),
		lastRunTime:     make(map[string]time.Time, len(tables)),
		lastRunError:    make(map[string]error, len(tables)),
		consecutiveErrs: make(map[string]int, len(tables)),
	}
}

// SetClock overrides the job's time source for deterministic testing.
// Call before Start(); production callers should leave the default.
func (j *AnalyzeJob) SetClock(c timeutil.Clock) {
	j.clock = c
}

// Start begins the ANALYZE loop. It is safe to call Start only once.
func (j *AnalyzeJob) Start() {
	j.ctx, j.cancel = context.WithCancel(context.Background())
	j.running.Store(1)

	j.wg.Add(1)
	go j.analyzeLoop()
}

// Stop signals the loop to exit and waits up to ShutdownTimeout for in-flight
// work to drain.
func (j *AnalyzeJob) Stop() error {
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
		return fmt.Errorf("analyze job shutdown timed out after %v", j.config.ShutdownTimeout)
	}
}

// Health returns nil if the job is running and not stuck in a failure loop.
func (j *AnalyzeJob) Health() error {
	if j.running.Load() != 1 {
		return errors.New("analyze job is not running")
	}

	j.mu.RLock()
	defer j.mu.RUnlock()

	for _, table := range j.config.tables() {
		if j.consecutiveErrs[table.name] > 5 {
			return fmt.Errorf("analyze job has %d consecutive errors on %s",
				j.consecutiveErrs[table.name], table.name)
		}
	}
	return nil
}

// AnalyzeStats reports aggregate analyze job statistics.
type AnalyzeStats struct {
	TotalAnalyzed   int64
	TotalFailed     int64
	LastRunTimes    map[string]time.Time
	LastRunErrors   map[string]string
	ConsecutiveErrs map[string]int
}

// GetStats returns a snapshot of the job's statistics.
func (j *AnalyzeJob) GetStats() AnalyzeStats {
	j.mu.RLock()
	defer j.mu.RUnlock()

	totalAnalyzed := int64(0)
	totalFailed := int64(0)
	for _, c := range j.analyzeCount {
		totalAnalyzed += c
	}
	for _, c := range j.failedCount {
		totalFailed += c
	}

	lrTimes := make(map[string]time.Time, len(j.lastRunTime))
	for k, v := range j.lastRunTime {
		lrTimes[k] = v
	}
	lrErrs := make(map[string]string, len(j.lastRunError))
	for k, v := range j.lastRunError {
		if v != nil {
			lrErrs[k] = v.Error()
		}
	}
	consec := make(map[string]int, len(j.consecutiveErrs))
	for k, v := range j.consecutiveErrs {
		consec[k] = v
	}

	return AnalyzeStats{
		TotalAnalyzed:   totalAnalyzed,
		TotalFailed:     totalFailed,
		LastRunTimes:    lrTimes,
		LastRunErrors:   lrErrs,
		ConsecutiveErrs: consec,
	}
}

// analyzeLoop runs the main ANALYZE loop.
func (j *AnalyzeJob) analyzeLoop() {
	defer j.wg.Done()

	ticker := time.NewTicker(j.config.TickInterval)
	defer ticker.Stop()

	// Run once immediately on startup.
	j.analyzeDue()

	for {
		select {
		case <-j.ctx.Done():
			return
		case <-ticker.C:
			j.analyzeDue()
		}
	}
}

// analyzeDue checks each hot table and runs ANALYZE if it is due.
func (j *AnalyzeJob) analyzeDue() {
	now := j.clock.Now()

	for _, table := range j.config.tables() {
		if !j.isDue(table, now) {
			continue
		}
		j.analyzeTable(table)
	}
}

// isDue returns true when a table has not been analyzed within its configured
// interval.
func (j *AnalyzeJob) isDue(table hotTable, now time.Time) bool {
	j.mu.RLock()
	last, exists := j.lastRunTime[table.name]
	j.mu.RUnlock()

	if !exists {
		return true
	}
	return now.Sub(last) >= table.interval
}

// analyzeTable runs ANALYZE on a single table and records the outcome.
func (j *AnalyzeJob) analyzeTable(table hotTable) {
	ctx, cancel := context.WithTimeout(j.ctx, j.config.AnalyzeTimeout)
	defer cancel()

	err := j.store.Analyze(ctx, table.name)

	j.mu.Lock()
	j.lastRunTime[table.name] = j.clock.Now()

	if err != nil {
		j.failedCount[table.name]++
		j.lastRunError[table.name] = err
		j.consecutiveErrs[table.name]++
		j.mu.Unlock()

		if j.logger != nil {
			j.logger.Error("analyze job error", "table", table.name, "error", err.Error())
		}
		return
	}

	j.analyzeCount[table.name]++
	j.lastRunError[table.name] = nil
	j.consecutiveErrs[table.name] = 0
	now := j.lastRunTime[table.name]
	j.mu.Unlock()

	// Push metric after releasing the lock so the gauge read is independent.
	metrics.AnalyzeLastRunTimestamp.WithLabelValues(table.name).Set(float64(now.Unix()))
}

// pgxAnalyzeStore is the production analyzeStore backed by *pgxpool.Pool.
type pgxAnalyzeStore struct {
	pool *pgxpool.Pool
}

func (s *pgxAnalyzeStore) Analyze(ctx context.Context, table string) error {
	_, err := s.pool.Exec(ctx, "ANALYZE "+table)
	return err
}
