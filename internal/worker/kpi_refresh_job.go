package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"stellarbill-backend/internal/metrics"
)

// kpiLogger is the minimal logging surface the KPI refresh job needs.
type kpiLogger interface {
	Error(msg string, keysAndValues ...any)
}

// KpiRefreshConfig configures the business-KPI metrics refresh job.
type KpiRefreshConfig struct {
	// PollInterval: how often KPI metrics are recomputed (default: 1h).
	PollInterval time.Duration
	// QueryTimeout: context timeout for a single metrics computation (default: 30s).
	QueryTimeout time.Duration
	// ShutdownTimeout: max time to wait for in-flight work on Stop() (default: 30s).
	ShutdownTimeout time.Duration
}

// DefaultKpiRefreshConfig returns production-safe defaults: an hourly
// refresh as required for business KPI monitoring.
func DefaultKpiRefreshConfig() KpiRefreshConfig {
	return KpiRefreshConfig{
		PollInterval:    1 * time.Hour,
		QueryTimeout:    30 * time.Second,
		ShutdownTimeout: 30 * time.Second,
	}
}

// withDefaults fills any zero-valued fields with their defaults.
func (c KpiRefreshConfig) withDefaults() KpiRefreshConfig {
	d := DefaultKpiRefreshConfig()
	if c.PollInterval <= 0 {
		c.PollInterval = d.PollInterval
	}
	if c.QueryTimeout <= 0 {
		c.QueryTimeout = d.QueryTimeout
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = d.ShutdownTimeout
	}
	return c
}

// kpiStore abstracts the database operations the KPI refresh job needs.
type kpiStore interface {
	// ComputeMRRInCents returns the total monthly recurring revenue of all
	// active subscriptions, expressed in cents. Returns 0 when there are no
	// active subscriptions.
	ComputeMRRInCents(ctx context.Context) (int64, error)
	// CountActiveSubscribersPerPlan returns the number of active subscriptions
	// grouped by plan_id and plan_name. Returns an empty map when there are no
	// active subscriptions.
	CountActiveSubscribersPerPlan(ctx context.Context) (map[KpiPlanKey]int64, error)
	// ComputeChurnRate24h returns the churn rate over the last 24 hours:
	// subscriptions cancelled in the window divided by the base of active +
	// recently-cancelled subscriptions. Returns 0.0 when the base is empty.
	ComputeChurnRate24h(ctx context.Context) (float64, error)
}

// KpiPlanKey identifies a plan in the active-subscribers breakdown.
type KpiPlanKey struct {
	ID   string
	Name string
}

// KpiRefreshJob periodically computes business KPI metrics (MRR, active
// subscribers, churn) and pushes them to Prometheus gauges so operators can
// graph business health on the same dashboard as system health.
//
// Computations run on a scheduled worker to avoid heavy aggregation queries
// at scrape time.
type KpiRefreshJob struct {
	store  kpiStore
	config KpiRefreshConfig
	logger kpiLogger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	running atomic.Int32

	// stats
	mu              sync.RWMutex
	refreshCount    int64
	failedCount     int64
	lastRunTime     time.Time
	lastRunError    error
	consecutiveErrs int
}

// NewKpiRefreshJob constructs a KPI refresh job backed by the given database.
func NewKpiRefreshJob(db *sql.DB, config KpiRefreshConfig, l kpiLogger) *KpiRefreshJob {
	return newKpiRefreshJob(&sqlKpiStore{db: db}, config, l)
}

// newKpiRefreshJob is the store-injecting constructor used by tests.
func newKpiRefreshJob(store kpiStore, config KpiRefreshConfig, l kpiLogger) *KpiRefreshJob {
	return &KpiRefreshJob{
		store:  store,
		config: config.withDefaults(),
		logger: l,
	}
}

// Start begins the refresh loop. It is safe to call Start only once.
func (j *KpiRefreshJob) Start() {
	j.ctx, j.cancel = context.WithCancel(context.Background())
	j.running.Store(1)

	j.wg.Add(1)
	go j.refreshLoop()
}

// Stop signals the refresh loop to exit and waits up to ShutdownTimeout for
// in-flight work to drain.
func (j *KpiRefreshJob) Stop() error {
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
		return fmt.Errorf("KPI refresh job shutdown timed out after %v", j.config.ShutdownTimeout)
	}
}

// Health returns nil if the job is running and not stuck in a failure loop.
func (j *KpiRefreshJob) Health() error {
	if j.running.Load() != 1 {
		return errors.New("KPI refresh job is not running")
	}

	j.mu.RLock()
	consec := j.consecutiveErrs
	j.mu.RUnlock()

	if consec > 5 {
		return fmt.Errorf("KPI refresh job has %d consecutive errors", consec)
	}
	return nil
}

// KpiRefreshStats reports KPI refresh job statistics.
type KpiRefreshStats struct {
	Refreshed      int64
	Failed         int64
	LastRunTime    time.Time
	LastRunError   string
	ConsecutiveErr int
}

// GetStats returns a snapshot of the job's statistics.
func (j *KpiRefreshJob) GetStats() KpiRefreshStats {
	j.mu.RLock()
	defer j.mu.RUnlock()

	errMsg := ""
	if j.lastRunError != nil {
		errMsg = j.lastRunError.Error()
	}
	return KpiRefreshStats{
		Refreshed:      j.refreshCount,
		Failed:         j.failedCount,
		LastRunTime:    j.lastRunTime,
		LastRunError:   errMsg,
		ConsecutiveErr: j.consecutiveErrs,
	}
}

// refreshLoop runs the main refresh loop.
func (j *KpiRefreshJob) refreshLoop() {
	defer j.wg.Done()

	ticker := time.NewTicker(j.config.PollInterval)
	defer ticker.Stop()

	// Compute immediately on startup so the dashboard shows data promptly.
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

// refreshOnce performs a single KPI computation and pushes results to
// Prometheus gauges. Errors are recorded via recordError.
func (j *KpiRefreshJob) refreshOnce() {
	ctx, cancel := context.WithTimeout(j.ctx, j.config.QueryTimeout)
	defer cancel()

	mrr, err := j.store.ComputeMRRInCents(ctx)
	if err != nil {
		j.recordError(fmt.Errorf("compute MRR: %w", err))
		return
	}

	planCounts, err := j.store.CountActiveSubscribersPerPlan(ctx)
	if err != nil {
		j.recordError(fmt.Errorf("count active subscribers: %w", err))
		return
	}

	churnRate, err := j.store.ComputeChurnRate24h(ctx)
	if err != nil {
		j.recordError(fmt.Errorf("compute churn rate: %w", err))
		return
	}

	// Push metrics to Prometheus gauges.
	metrics.MrrCents.Set(float64(mrr))

	// Reset the gauge vec to clear stale plan labels, then set current values.
	metrics.ActiveSubscribersTotal.Reset()
	for key, count := range planCounts {
		metrics.ActiveSubscribersTotal.WithLabelValues(key.ID, key.Name).Set(float64(count))
	}

	metrics.ChurnRate24h.Set(churnRate)

	now := time.Now().UTC()
	j.mu.Lock()
	j.refreshCount++
	j.lastRunTime = now
	j.lastRunError = nil
	j.consecutiveErrs = 0
	j.mu.Unlock()
}

func (j *KpiRefreshJob) recordError(err error) {
	j.mu.Lock()
	j.failedCount++
	j.lastRunError = err
	j.consecutiveErrs++
	j.mu.Unlock()

	if j.logger != nil {
		j.logger.Error("KPI refresh job error", "error", err.Error())
	}
}

// sqlKpiStore is the production kpiStore backed by *sql.DB.
type sqlKpiStore struct {
	db *sql.DB
}

func (s *sqlKpiStore) ComputeMRRInCents(ctx context.Context) (int64, error) {
	var cents sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount * 100), 0)::bigint FROM subscriptions WHERE status = 'active'`,
	).Scan(&cents)
	if err != nil {
		return 0, err
	}
	if cents.Valid {
		return cents.Int64, nil
	}
	return 0, nil
}

type planCountRow struct {
	PlanID   string
	PlanName string
	Count    int64
}

func (s *sqlKpiStore) CountActiveSubscribersPerPlan(ctx context.Context) (map[KpiPlanKey]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT plan_id, COALESCE(plan_name, plan_id), COUNT(*)::bigint
		 FROM subscriptions
		 WHERE status = 'active'
		 GROUP BY plan_id, plan_name
		 ORDER BY plan_id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[KpiPlanKey]int64)
	for rows.Next() {
		var row planCountRow
		if err := rows.Scan(&row.PlanID, &row.PlanName, &row.Count); err != nil {
			return nil, err
		}
		result[KpiPlanKey{ID: row.PlanID, Name: row.PlanName}] = row.Count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *sqlKpiStore) ComputeChurnRate24h(ctx context.Context) (float64, error) {
	var rate sql.NullFloat64
	err := s.db.QueryRowContext(ctx,
		`SELECT
		   COUNT(*) FILTER (WHERE status = 'cancelled' AND cancelled_at >= NOW() - INTERVAL '24 hours')::float
		   /
		   NULLIF(
		     COUNT(*) FILTER (WHERE status IN ('active', 'cancelled')), 0
		   )::float
		 FROM subscriptions
		 WHERE status IN ('active', 'cancelled')`,
	).Scan(&rate)
	if err != nil {
		return 0, err
	}
	if rate.Valid {
		return rate.Float64, nil
	}
	return 0, nil
}