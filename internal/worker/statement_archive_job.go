package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"stellarbill-backend/internal/cache"
	"stellarbill-backend/internal/featureflags"
	"stellarbill-backend/internal/logger"
	"stellarbill-backend/internal/repository"
)

// StatementArchiveConfig holds configuration for the statement archival job.
type StatementArchiveConfig struct {
	// ArchiveThresholdMonths: statements older than this are eligible for archival (default: 24)
	ArchiveThresholdMonths int
	// BatchSize: number of statements to archive per batch (default: 100)
	BatchSize int
	// ObjectKeyPrefix: S3-like prefix for archived statement keys (e.g., "statements/archive/")
	ObjectKeyPrefix string
	// PollInterval: how often to run the archival job (default: 24h)
	PollInterval time.Duration
	// ArchiveTimeout: context timeout per batch operation (default: 5m)
	ArchiveTimeout time.Duration
	// ShutdownTimeout: max time to wait for in-flight work on Stop() (default: 30s)
	ShutdownTimeout time.Duration
}

// DefaultStatementArchiveConfig returns production-safe defaults.
func DefaultStatementArchiveConfig() StatementArchiveConfig {
	return StatementArchiveConfig{
		ArchiveThresholdMonths: 24,
		BatchSize:              100,
		ObjectKeyPrefix:        "statements/archive/",
		PollInterval:           24 * time.Hour,
		ArchiveTimeout:         5 * time.Minute,
		ShutdownTimeout:        30 * time.Second,
	}
}

// StatementArchiveJob manages archival of old statements to cold storage.
// It uses cursor-based scanning to efficiently process large result sets.
type StatementArchiveJob struct {
	db       *sql.DB
	objStore cache.ObjectStore
	config   StatementArchiveConfig
	logger   logger.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// running is set to 1 between Start and Stop.
	running atomic.Int32

	// stats
	mu              sync.RWMutex
	archivedCount   int64
	failedCount     int64
	lastRunTime     time.Time
	lastRunError    error
	consecutiveErrs int
}

// NewStatementArchiveJob creates a new statement archival job.
func NewStatementArchiveJob(db *sql.DB, objStore cache.ObjectStore, config StatementArchiveConfig, l logger.Logger) *StatementArchiveJob {
	return &StatementArchiveJob{
		db:       db,
		objStore: objStore,
		config:   config,
		logger:   l,
	}
}

// Start begins the archival loop. It is safe to call Start only once.
func (j *StatementArchiveJob) Start() {
	j.ctx, j.cancel = context.WithCancel(context.Background())
	j.running.Store(1)

	j.wg.Add(1)
	go j.archiveLoop()
}

// Stop signals the archival loop to exit and waits for in-flight work to drain
// up to ShutdownTimeout.
func (j *StatementArchiveJob) Stop() error {
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
		return fmt.Errorf("statement archive job shutdown timed out after %v", j.config.ShutdownTimeout)
	}
}

// Health returns nil if healthy, error otherwise.
// A job is unhealthy if it has too many consecutive errors.
func (j *StatementArchiveJob) Health() error {
	if j.running.Load() != 1 {
		return fmt.Errorf("statement archive job is not running")
	}

	j.mu.RLock()
	consec := j.consecutiveErrs
	j.mu.RUnlock()

	if consec > 5 {
		return fmt.Errorf("statement archive job has %d consecutive errors", consec)
	}

	return nil
}

// Stats returns archival job statistics.
type StatementArchiveStats struct {
	Archived       int64
	Failed         int64
	LastRunTime    time.Time
	LastRunError   string
	ConsecutiveErr int
}

// GetStats returns current archival job statistics.
func (j *StatementArchiveJob) GetStats() StatementArchiveStats {
	j.mu.RLock()
	defer j.mu.RUnlock()

	errMsg := ""
	if j.lastRunError != nil {
		errMsg = j.lastRunError.Error()
	}

	return StatementArchiveStats{
		Archived:       j.archivedCount,
		Failed:         j.failedCount,
		LastRunTime:    j.lastRunTime,
		LastRunError:   errMsg,
		ConsecutiveErr: j.consecutiveErrs,
	}
}

// archiveLoop runs the main archival loop.
func (j *StatementArchiveJob) archiveLoop() {
	defer j.wg.Done()

	ticker := time.NewTicker(j.config.PollInterval)
	defer ticker.Stop()

	// Run once immediately on startup
	j.archiveBatch()

	for {
		select {
		case <-j.ctx.Done():
			return
		case <-ticker.C:
			j.archiveBatch()
		}
	}
}

// archiveBatch processes one batch of old statements.
func (j *StatementArchiveJob) archiveBatch() {
	batchCtx, cancel := context.WithTimeout(j.ctx, j.config.ArchiveTimeout)
	defer cancel()

	threshold := time.Now().AddDate(0, -j.config.ArchiveThresholdMonths, 0)
	thresholdStr := threshold.Format(time.RFC3339)

	tableName := "statements"
	if featureflags.IsEnabled("statements_partitioning") {
		tableName = "statements_partitioned"
	}

	query := fmt.Sprintf(`SELECT id, subscription_id, customer_id, period_start, period_end, 
		        issued_at, total_amount, currency, kind, status
		 FROM %s
		 WHERE archived_at IS NULL 
		   AND deleted_at IS NULL 
		   AND issued_at < $1
		   AND period_start < $1
		 ORDER BY issued_at ASC
		 LIMIT $2`, tableName)

	rows, err := j.db.QueryContext(
		batchCtx,
		query,
		thresholdStr,
		j.config.BatchSize,
	)
	if err != nil {
		j.recordError(err)
		return
	}
	defer rows.Close()

	var stmts []*repository.StatementRow
	for rows.Next() {
		var stmt repository.StatementRow
		err := rows.Scan(
			&stmt.ID,
			&stmt.SubscriptionID,
			&stmt.CustomerID,
			&stmt.PeriodStart,
			&stmt.PeriodEnd,
			&stmt.IssuedAt,
			&stmt.TotalAmount,
			&stmt.Currency,
			&stmt.Kind,
			&stmt.Status,
		)
		if err != nil {
			j.recordError(fmt.Errorf("scan statement row: %w", err))
			return
		}
		stmts = append(stmts, &stmt)
	}

	if err := rows.Err(); err != nil {
		j.recordError(err)
		return
	}

	if len(stmts) == 0 {
		j.resetErrorCount()
		return
	}

	// Archive each statement
	for _, stmt := range stmts {
		if err := j.archiveStatement(batchCtx, stmt); err != nil {
			j.recordError(fmt.Errorf("archive statement %s: %w", stmt.ID, err))
			continue
		}
	}

	j.mu.Lock()
	j.lastRunTime = time.Now()
	j.mu.Unlock()
}

// archiveStatement archives a single statement to object storage.
func (j *StatementArchiveJob) archiveStatement(ctx context.Context, stmt *repository.StatementRow) error {
	// Serialize to JSON
	now := time.Now().UTC()
	payload := &cache.StatementArchivePayload{
		ID:             stmt.ID,
		SubscriptionID: stmt.SubscriptionID,
		CustomerID:     stmt.CustomerID,
		PeriodStart:    stmt.PeriodStart,
		PeriodEnd:      stmt.PeriodEnd,
		IssuedAt:       stmt.IssuedAt,
		TotalAmount:    stmt.TotalAmount,
		Currency:       stmt.Currency,
		Kind:           stmt.Kind,
		Status:         stmt.Status,
		ArchivedAt:     now.Format(time.RFC3339),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	// Generate object key: prefix + YYYY/MM/DD/statement_id.json
	key := fmt.Sprintf("%s%d/%02d/%02d/%s.json",
		j.config.ObjectKeyPrefix,
		now.Year(), now.Month(), now.Day(),
		stmt.ID,
	)

	// Upload to object storage
	_, err = j.objStore.Put(ctx, key, data)
	if err != nil {
		return fmt.Errorf("upload to object store: %w", err)
	}

	tableName := "statements"
	if featureflags.IsEnabled("statements_partitioning") {
		tableName = "statements_partitioned"
	}

	// Update row in database: clear data and set archived_at + archive_key
	_, err = j.db.ExecContext(
		ctx,
		fmt.Sprintf(`UPDATE %s
		 SET archived_at = $1,
		     archive_key = $2,
		     period_start = NULL,
		     period_end = NULL,
		     issued_at = NULL,
		     total_amount = NULL,
		     currency = NULL,
		     kind = NULL,
		     status = NULL
		 WHERE id = $3 AND period_start = $4`, tableName),
		now,
		key,
		stmt.ID,
		stmt.PeriodStart,
	)
	if err != nil {
		// Attempt to delete from object store on failure (cleanup)
		delErr := j.objStore.Delete(ctx, key)
		if delErr != nil {
			j.logger.Warn("Failed to cleanup object after DB update failure",
				"statement_id", stmt.ID,
				"key", key,
				"cleanup_error", delErr.Error(),
			)
		}
		return fmt.Errorf("update database: %w", err)
	}

	j.mu.Lock()
	j.archivedCount++
	j.mu.Unlock()

	return nil
}

// recordError records an archival error and increments error counter.
func (j *StatementArchiveJob) recordError(err error) {
	j.mu.Lock()
	j.failedCount++
	j.lastRunError = err
	j.consecutiveErrs++
	j.mu.Unlock()

	if j.logger != nil {
		j.logger.Error("Statement archive job error", "error", err.Error())
	}
}

// resetErrorCount resets the consecutive error counter on success.
func (j *StatementArchiveJob) resetErrorCount() {
	j.mu.Lock()
	j.consecutiveErrs = 0
	j.lastRunError = nil
	j.mu.Unlock()
}
