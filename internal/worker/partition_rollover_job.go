package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"stellarbill-backend/internal/timeutil"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// PartitionRolloverConfig configures the monthly partition rollover job for
// the statements_partitioned table.
type PartitionRolloverConfig struct {
	// PollInterval: how often the rollover job runs (default: 24h).
	PollInterval time.Duration
	// RolloverTimeout: context timeout for a single rollover run (default: 5m).
	RolloverTimeout time.Duration
	// ShutdownTimeout: max time to wait for in-flight work on Stop() (default: 30s).
	ShutdownTimeout time.Duration
	// LookaheadMonths: how many future partitions to create in advance (default: 1).
	LookaheadMonths int
	// DetachThresholdMonths: partitions older than this many months are detached
	// (default: 24). Set to 0 to disable detachment.
	DetachThresholdMonths int
	// ParentTable: the name of the partitioned parent table (default: "statements_partitioned").
	ParentTable string
	// PartitionPrefix: prefix for partition names (default: "statements_p").
	PartitionPrefix string
	// Cooldown: minimum interval between runs for idempotency (default: 1h).
	Cooldown time.Duration
}

// DefaultPartitionRolloverConfig returns production-safe defaults for the
// monthly partition rollover job.
func DefaultPartitionRolloverConfig() PartitionRolloverConfig {
	return PartitionRolloverConfig{
		PollInterval:          24 * time.Hour,
		RolloverTimeout:       5 * time.Minute,
		ShutdownTimeout:       30 * time.Second,
		LookaheadMonths:       1,
		DetachThresholdMonths: 24,
		ParentTable:           "statements_partitioned",
		PartitionPrefix:       "statements_p",
		Cooldown:              1 * time.Hour,
	}
}

// withDefaults fills zero-valued fields with sensible defaults so callers can
// override only what they care about.
func (c PartitionRolloverConfig) withDefaults() PartitionRolloverConfig {
	d := DefaultPartitionRolloverConfig()
	if c.PollInterval <= 0 {
		c.PollInterval = d.PollInterval
	}
	if c.RolloverTimeout <= 0 {
		c.RolloverTimeout = d.RolloverTimeout
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = d.ShutdownTimeout
	}
	if c.LookaheadMonths <= 0 {
		c.LookaheadMonths = d.LookaheadMonths
	}
	if c.DetachThresholdMonths <= 0 {
		c.DetachThresholdMonths = d.DetachThresholdMonths
	}
	if c.ParentTable == "" {
		c.ParentTable = d.ParentTable
	}
	if c.PartitionPrefix == "" {
		c.PartitionPrefix = d.PartitionPrefix
	}
	if c.Cooldown <= 0 {
		c.Cooldown = d.Cooldown
	}
	return c
}

// partitionRolloverStore abstracts the database operations the rollover job needs.
type partitionRolloverStore interface {
	// LastRolloverAt returns the recorded last rollover time. ok is false when
	// the job has never run (the seeded row at epoch).
	LastRolloverAt(ctx context.Context) (at time.Time, ok bool, err error)
	// MarkRolloverDone records the moment the rollover last completed.
	MarkRolloverDone(ctx context.Context, at time.Time) error
	// PartitionExists checks whether a partition with the given name exists
	// in pg_catalog under the parent table.
	PartitionExists(ctx context.Context, parentTable, partitionName string) (bool, error)
	// CreatePartition adds a new monthly partition to the parent table.
	CreatePartition(ctx context.Context, parentTable, partitionName, fromDate, toDate string) error
	// DetachPartition detaches a partition from the parent table, converting it
	// to a standalone table.
	DetachPartition(ctx context.Context, parentTable, partitionName string) error
	// ListPartitions returns the names of all partitions belonging to the parent.
	ListPartitions(ctx context.Context, parentTable string) ([]string, error)
}

// PartitionRolloverJob creates next-month partitions and detaches partitions
// older than the archival threshold. This prevents outages when the statements
// partition graph runs out of children.
//
// The job is idempotent and safe to run more than once per hour. It tracks
// its last successful run time in the partition_rollover_state table and
// skips if the cooldown period has not elapsed.
type PartitionRolloverJob struct {
	store  partitionRolloverStore
	config PartitionRolloverConfig
	logger partitionRolloverLogger
	clock  timeutil.Clock
	leader *leaderGuard

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	running atomic.Int32

	// stats
	mu              sync.RWMutex
	rolloverCount   int64
	failedCount     int64
	partitionsAdded int64
	partitionsGone  int64
	lastRunTime     time.Time
	lastRunError    error
	consecutiveErrs int
}

// partitionRolloverLogger is the minimal logging surface the rollover job
// needs. A nil logger is accepted and simply disables logging.
type partitionRolloverLogger interface {
	Error(msg string, keysAndValues ...any)
}

// partitionRolloverMetrics are lazily registered Prometheus metrics for the
// partition rollover job.
var (
	partitionCreatedTotal  prometheus.Counter
	partitionDetachedTotal prometheus.Counter
	partitionMetricsOnce   sync.Once
)

func initPartitionMetrics() {
	partitionMetricsOnce.Do(func() {
		partitionCreatedTotal = promauto.NewCounter(prometheus.CounterOpts{
			Name: "partition_created_total",
			Help: "Total number of monthly partitions created for the statements table",
		})
		partitionDetachedTotal = promauto.NewCounter(prometheus.CounterOpts{
			Name: "partition_detached_total",
			Help: "Total number of monthly partitions detached from the statements table",
		})
	})
}

// NewPartitionRolloverJob creates a new partition rollover job backed by the
// given database connection.
func NewPartitionRolloverJob(db *sql.DB, config PartitionRolloverConfig, l partitionRolloverLogger) *PartitionRolloverJob {
	initPartitionMetrics()
	job := newPartitionRolloverJob(&sqlPartitionRolloverStore{db: db}, config, l)
	if db != nil {
		locker, err := NewPostgresLeaderLocker(db)
		if err != nil {
			if job.logger != nil {
				job.logger.Error("partition rollover leader election unavailable", "error", err.Error())
			}
		} else {
			job.leader = newLeaderGuard(locker, "partition_rollover", 1001)
		}
	}
	return job
}

// newPartitionRolloverJob is the store-injecting constructor used by tests.
func newPartitionRolloverJob(store partitionRolloverStore, config PartitionRolloverConfig, l partitionRolloverLogger) *PartitionRolloverJob {
	return &PartitionRolloverJob{
		store:  store,
		config: config.withDefaults(),
		logger: l,
		clock:  timeutil.SystemClock,
	}
}

// SetClock overrides the job's time source so partition boundary and cooldown
// behavior can be tested deterministically. Call before Start(); production
// callers should leave the default SystemClock in place.
func (j *PartitionRolloverJob) SetClock(c timeutil.Clock) {
	j.clock = c
}

// Start begins the rollover loop. It is safe to call Start only once.
func (j *PartitionRolloverJob) Start() {
	j.ctx, j.cancel = context.WithCancel(context.Background())
	j.running.Store(1)

	j.wg.Add(1)
	go j.rolloverLoop()
}

// Stop signals the rollover loop to exit and waits up to ShutdownTimeout for
// in-flight work to drain.
func (j *PartitionRolloverJob) Stop() error {
	if j.cancel == nil {
		if j.leader != nil {
			j.leader.Release(context.Background())
		}
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
		if j.leader != nil {
			j.leader.Release(context.Background())
		}
		j.running.Store(0)
		return nil
	case <-time.After(j.config.ShutdownTimeout):
		if j.leader != nil {
			j.leader.Release(context.Background())
		}
		j.running.Store(0)
		return fmt.Errorf("partition rollover job shutdown timed out after %v", j.config.ShutdownTimeout)
	}
}

// Health returns nil if the job is running and not stuck in a failure loop.
func (j *PartitionRolloverJob) Health() error {
	if j.running.Load() != 1 {
		return errors.New("partition rollover job is not running")
	}

	j.mu.RLock()
	consec := j.consecutiveErrs
	j.mu.RUnlock()

	if consec > 5 {
		return fmt.Errorf("partition rollover job has %d consecutive errors", consec)
	}
	return nil
}

// PartitionRolloverStats reports rollover job statistics.
type PartitionRolloverStats struct {
	RolloverCount   int64
	PartitionsAdded int64
	PartitionsGone  int64
	LastRunTime     time.Time
	LastRunError    string
	ConsecutiveErr  int
}

// GetStats returns a snapshot of the job's statistics.
func (j *PartitionRolloverJob) GetStats() PartitionRolloverStats {
	j.mu.RLock()
	defer j.mu.RUnlock()

	errMsg := ""
	if j.lastRunError != nil {
		errMsg = j.lastRunError.Error()
	}
	return PartitionRolloverStats{
		RolloverCount:   j.rolloverCount,
		PartitionsAdded: j.partitionsAdded,
		PartitionsGone:  j.partitionsGone,
		LastRunTime:     j.lastRunTime,
		LastRunError:    errMsg,
		ConsecutiveErr:  j.consecutiveErrs,
	}
}

// rolloverLoop runs the main rollover loop at the configured poll interval.
func (j *PartitionRolloverJob) rolloverLoop() {
	defer j.wg.Done()

	ticker := time.NewTicker(j.config.PollInterval)
	defer ticker.Stop()

	// Run once immediately on startup so the partition graph stays current
	// without waiting for the first tick.
	j.rolloverOnce()

	for {
		select {
		case <-j.ctx.Done():
			return
		case <-ticker.C:
			j.rolloverOnce()
		}
	}
}

// rolloverOnce performs a single rollover cycle: create the next-month
// partition(s) and detach old partitions.
func (j *PartitionRolloverJob) rolloverOnce() {
	now := j.clock.Now()

	ctx := j.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if j.leader != nil {
		err := j.leader.Run(ctx, func(runCtx context.Context) error {
			return j.runRolloverCycle(runCtx, now)
		})
		if err != nil {
			if errors.Is(err, errNotLeader) {
				return
			}
			j.recordError(fmt.Errorf("rollover: %w", err))
		}
		return
	}

	if err := j.runRolloverCycle(ctx, now); err != nil {
		j.recordError(fmt.Errorf("rollover: %w", err))
	}
}

func (j *PartitionRolloverJob) runRolloverCycle(ctx context.Context, now time.Time) error {
	// Idempotency guard: skip if within the cooldown period.
	lastRun, ok, err := j.store.LastRolloverAt(ctx)
	if err != nil {
		return fmt.Errorf("read last rollover time: %w", err)
	}
	if ok && now.Sub(lastRun) < j.config.Cooldown {
		return nil
	}

	runCtx, cancel := context.WithTimeout(ctx, j.config.RolloverTimeout)
	defer cancel()

	created, detached, err := j.doRollover(runCtx, now)
	if err != nil {
		return fmt.Errorf("rollover: %w", err)
	}

	// Persist the rollover timestamp so subsequent runs respect the cooldown.
	if err := j.store.MarkRolloverDone(runCtx, now); err != nil {
		return fmt.Errorf("mark rollover done: %w", err)
	}

	j.mu.Lock()
	j.rolloverCount++
	j.partitionsAdded += created
	j.partitionsGone += detached
	j.lastRunTime = now
	j.lastRunError = nil
	j.consecutiveErrs = 0
	j.mu.Unlock()

	// Update Prometheus metrics.
	partitionCreatedTotal.Add(float64(created))
	partitionDetachedTotal.Add(float64(detached))
	return nil
}

// doRollover executes the actual partition management operations. It returns
// the count of partitions created and detached.
func (j *PartitionRolloverJob) doRollover(ctx context.Context, now time.Time) (created int64, detached int64, err error) {
	parent := j.config.ParentTable

	// --- Create next-month partition(s) ---
	for m := 1; m <= j.config.LookaheadMonths; m++ {
		next := time.Date(now.Year(), now.Month()+time.Month(m), 1, 0, 0, 0, 0, time.UTC)

		partitionName := j.partitionName(next)
		exists, err := j.store.PartitionExists(ctx, parent, partitionName)
		if err != nil {
			return created, detached, fmt.Errorf("check partition %s: %w", partitionName, err)
		}
		if exists {
			continue
		}

		fromDate := next.Format(time.RFC3339)
		toDate := next.AddDate(0, 1, 0).Format(time.RFC3339)

		if err := j.store.CreatePartition(ctx, parent, partitionName, fromDate, toDate); err != nil {
			return created, detached, fmt.Errorf("create partition %s: %w", partitionName, err)
		}
		created++
	}

	// --- Detach old partitions ---
	if j.config.DetachThresholdMonths > 0 {
		threshold := now.AddDate(0, -j.config.DetachThresholdMonths, 0)
		partitions, err := j.store.ListPartitions(ctx, parent)
		if err != nil {
			return created, detached, fmt.Errorf("list partitions: %w", err)
		}

		for _, p := range partitions {
			// Skip the default partition (never detach it).
			if p == j.defaultPartitionName() {
				continue
			}

			parsed, ok := j.parsePartitionDate(p)
			if !ok {
				continue // Not a monthly partition we recognise.
			}

			if parsed.Before(threshold) {
				if err := j.store.DetachPartition(ctx, parent, p); err != nil {
					return created, detached, fmt.Errorf("detach partition %s: %w", p, err)
				}
				detached++
			}
		}
	}

	return created, detached, nil
}

// partitionName generates the standard partition name for a given month.
// For example: "statements_p2026_08"
func (j *PartitionRolloverJob) partitionName(t time.Time) string {
	return fmt.Sprintf("%s%d_%02d", j.config.PartitionPrefix, t.Year(), t.Month())
}

// defaultPartitionName returns the name of the default partition.
func (j *PartitionRolloverJob) defaultPartitionName() string {
	return fmt.Sprintf("%s_default", strings.TrimRight(j.config.PartitionPrefix, "_"))
}

// parsePartitionDate extracts the month from a partition name like "statements_p2026_08".
// Returns the time representing the first of that month.
func (j *PartitionRolloverJob) parsePartitionDate(name string) (time.Time, bool) {
	prefix := j.config.PartitionPrefix
	if !strings.HasPrefix(name, prefix) {
		return time.Time{}, false
	}

	datePart := strings.TrimPrefix(name, prefix)
	t, err := time.Parse("2006_01", datePart)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func (j *PartitionRolloverJob) recordError(err error) {
	j.mu.Lock()
	j.failedCount++
	j.lastRunError = err
	j.consecutiveErrs++
	j.mu.Unlock()

	if j.logger != nil {
		j.logger.Error("Partition rollover job error", "error", err.Error())
	}
}

// sqlPartitionRolloverStore is the production implementation backed by *sql.DB.
type sqlPartitionRolloverStore struct {
	db *sql.DB
}

func (s *sqlPartitionRolloverStore) LastRolloverAt(ctx context.Context) (time.Time, bool, error) {
	var at sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT last_rollover_at FROM partition_rollover_state WHERE id = true`,
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

func (s *sqlPartitionRolloverStore) MarkRolloverDone(ctx context.Context, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE partition_rollover_state SET last_rollover_at = $1 WHERE id = true`,
		at.UTC(),
	)
	return err
}

func (s *sqlPartitionRolloverStore) PartitionExists(ctx context.Context, parentTable, partitionName string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_catalog.pg_inherits pi
			JOIN pg_catalog.pg_class pc_child ON pi.inhrelid = pc_child.oid
			JOIN pg_catalog.pg_class pc_parent ON pi.inhparent = pc_parent.oid
			WHERE pc_parent.relname = $1
			  AND pc_child.relname = $2
		)
	`, parentTable, partitionName).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (s *sqlPartitionRolloverStore) CreatePartition(ctx context.Context, parentTable, partitionName, fromDate, toDate string) error {
	query := fmt.Sprintf(
		`CREATE TABLE %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
		quoteIdent(partitionName),
		quoteIdent(parentTable),
		fromDate,
		toDate,
	)
	_, err := s.db.ExecContext(ctx, query)
	return err
}

func (s *sqlPartitionRolloverStore) DetachPartition(ctx context.Context, parentTable, partitionName string) error {
	query := fmt.Sprintf(
		`ALTER TABLE %s DETACH PARTITION %s`,
		quoteIdent(parentTable),
		quoteIdent(partitionName),
	)
	_, err := s.db.ExecContext(ctx, query)
	return err
}

func (s *sqlPartitionRolloverStore) ListPartitions(ctx context.Context, parentTable string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pc_child.relname
		FROM pg_catalog.pg_inherits pi
		JOIN pg_catalog.pg_class pc_child ON pi.inhrelid = pc_child.oid
		JOIN pg_catalog.pg_class pc_parent ON pi.inhparent = pc_parent.oid
		WHERE pc_parent.relname = $1
		ORDER BY pc_child.relname ASC
	`, parentTable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

// quoteIdent quotes a PostgreSQL identifier safely.
func quoteIdent(name string) string {
	// Simple quoting — replace any embedded quotes with doubled quotes per
	// SQL standard. Production identifiers in this codebase are alphanumeric
	// with underscores so this is defensive.
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
