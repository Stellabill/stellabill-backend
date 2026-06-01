package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// OutboxEvent mirrors the outbox_events table schema.
type OutboxEvent struct {
	ID            uuid.UUID       `json:"id"`
	EventType     string          `json:"event_type"`
	EventData     json.RawMessage `json:"event_data"`
	AggregateID   *string         `json:"aggregate_id,omitempty"`
	AggregateType *string         `json:"aggregate_type,omitempty"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Status        string          `json:"status"`
	RetryCount    int             `json:"retry_count"`
	MaxRetries    int             `json:"max_retries"`
	NextRetryAt   *time.Time      `json:"next_retry_at,omitempty"`
	ErrorMessage  *string         `json:"error_message,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Version       int             `json:"version"`
}

// Outbox event statuses matching the CHECK constraint in 0002_create_outbox.up.sql.
const (
	OutboxStatusPending    = "pending"
	OutboxStatusProcessing = "processing"
	OutboxStatusCompleted  = "completed"
	OutboxStatusFailed     = "failed"
)

// EventPublisher is the minimal interface that the outbox worker calls for
// each claimed event. Implementations can log, forward via HTTP, enqueue in
// an in-process channel, etc.
type EventPublisher interface {
	Publish(ctx context.Context, event *OutboxEvent) error
}

// OutboxWorkerConfig holds configuration for the OutboxWorker.
type OutboxWorkerConfig struct {
	PollInterval      time.Duration // how often the worker polls for events
	BatchSize         int           // max events to claim per poll cycle
	MaxRetries        int           // max retries before dead-letter (failed permanently)
	RetryBackoffBase  time.Duration // base for exponential backoff: base * 2^retryCount
	ShutdownTimeout   time.Duration // max time to wait for in-flight work on Stop()
	ProcessingTimeout time.Duration // context timeout per-event processing
}

// DefaultOutboxWorkerConfig returns production-safe defaults.
func DefaultOutboxWorkerConfig() OutboxWorkerConfig {
	return OutboxWorkerConfig{
		PollInterval:      5 * time.Second,
		BatchSize:         10,
		MaxRetries:        3,
		RetryBackoffBase:  1 * time.Second,
		ShutdownTimeout:   30 * time.Second,
		ProcessingTimeout: 30 * time.Second,
	}
}

// OutboxWorker polls outbox_events and processes them using FOR UPDATE SKIP
// LOCKED so that multiple concurrent workers never claim the same row.
//
// It satisfies handlers.OutboxHealther via Health() and GetStats().
type OutboxWorker struct {
	db        *sql.DB
	publisher EventPublisher
	config    OutboxWorkerConfig

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// running is set to 1 between Start and Stop.
	running atomic.Int32

	// stats
	mu              sync.RWMutex
	processed       int64
	succeeded       int64
	failed          int64
	deadLettered    int64
	lastPollTime    time.Time
	consecutiveErrs int
}

// NewOutboxWorker creates a new outbox worker.
func NewOutboxWorker(db *sql.DB, publisher EventPublisher, config OutboxWorkerConfig) *OutboxWorker {
	return &OutboxWorker{
		db:        db,
		publisher: publisher,
		config:    config,
	}
}

// Start begins the poll loop. It is safe to call Start only once.
func (w *OutboxWorker) Start() {
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.running.Store(1)

	w.wg.Add(1)
	go w.pollLoop()
}

// Stop signals the poll loop to exit and waits for in-flight work to drain
// up to ShutdownTimeout.
func (w *OutboxWorker) Stop() error {
	if w.cancel == nil {
		return nil
	}
	w.cancel()

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		w.running.Store(0)
		return nil
	case <-time.After(w.config.ShutdownTimeout):
		w.running.Store(0)
		return fmt.Errorf("outbox worker shutdown timed out after %v", w.config.ShutdownTimeout)
	}
}

// Health satisfies handlers.OutboxHealther.
// Returns nil when healthy, error when not.
func (w *OutboxWorker) Health() error {
	if w.running.Load() != 1 {
		return fmt.Errorf("outbox worker is not running")
	}

	w.mu.RLock()
	consec := w.consecutiveErrs
	w.mu.RUnlock()

	if consec > 5 {
		return fmt.Errorf("outbox worker has %d consecutive poll errors", consec)
	}
	return nil
}

// GetStats satisfies handlers.OutboxHealther.
func (w *OutboxWorker) GetStats() (map[string]interface{}, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return map[string]interface{}{
		"running":            w.running.Load() == 1,
		"processed":          w.processed,
		"succeeded":          w.succeeded,
		"failed":             w.failed,
		"dead_lettered":      w.deadLettered,
		"last_poll_time":     w.lastPollTime.Format(time.RFC3339),
		"consecutive_errors": w.consecutiveErrs,
	}, nil
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (w *OutboxWorker) pollLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.poll()
		}
	}
}

func (w *OutboxWorker) poll() {
	w.mu.Lock()
	w.lastPollTime = time.Now()
	w.mu.Unlock()

	events, err := w.claimBatch(w.ctx)
	if err != nil {
		w.mu.Lock()
		w.consecutiveErrs++
		w.mu.Unlock()
		return
	}

	// If claimBatch returned without error but with no events (nil),
	// treat this as a neutral poll: do not reset the consecutive error
	// counter so that transient recoveries which return no rows do not
	// immediately clear a preceding error streak. Only a successful
	// claim that returns actual events clears the error streak.
	if events == nil {
		return
	}

	w.mu.Lock()
	w.consecutiveErrs = 0
	w.mu.Unlock()

	for _, event := range events {
		// Respect shutdown between events.
		select {
		case <-w.ctx.Done():
			return
		default:
		}
		w.processEvent(event)
	}
}

// claimBatch atomically claims a batch of eligible events using
// FOR UPDATE SKIP LOCKED within a single transaction.
// Each claimed row is set to 'processing' inside the same transaction.
func (w *OutboxWorker) claimBatch(ctx context.Context) ([]*OutboxEvent, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Claim eligible rows: pending, or failed with next_retry_at in the past.
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
		       occurred_at, status, retry_count, max_retries, next_retry_at,
		       error_message, created_at, updated_at, version
		FROM outbox_events
		WHERE (status = $1)
		   OR (status = $2 AND next_retry_at IS NOT NULL AND next_retry_at <= $3)
		ORDER BY occurred_at ASC
		LIMIT $4
		FOR UPDATE SKIP LOCKED
	`

	rows, err := tx.QueryContext(ctx, query,
		OutboxStatusPending,
		OutboxStatusFailed,
		time.Now(),
		w.config.BatchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending: %w", err)
	}

	var events []*OutboxEvent
	for rows.Next() {
		e, scanErr := scanOutboxEvent(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	rows.Close()

	if len(events) == 0 {
		return nil, nil
	}

	// Mark them all as 'processing' inside the same transaction.
	for _, e := range events {
		_, err := tx.ExecContext(ctx,
			`UPDATE outbox_events SET status = $1, updated_at = NOW() WHERE id = $2`,
			OutboxStatusProcessing, e.ID,
		)
		if err != nil {
			return nil, fmt.Errorf("mark processing %s: %w", e.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim: %w", err)
	}

	return events, nil
}

func (w *OutboxWorker) processEvent(event *OutboxEvent) {
	w.mu.Lock()
	w.processed++
	w.mu.Unlock()

	ctx, cancel := context.WithTimeout(w.ctx, w.config.ProcessingTimeout)
	defer cancel()

	err := w.publisher.Publish(ctx, event)
	if err != nil {
		w.handleFailure(event, err)
		return
	}

	w.markCompleted(event)
}

func (w *OutboxWorker) markCompleted(event *OutboxEvent) {
	_, err := w.db.Exec(
		`UPDATE outbox_events
		 SET status = $1, updated_at = NOW()
		 WHERE id = $2`,
		OutboxStatusCompleted, event.ID,
	)
	if err != nil {
		// Log but don't lose the event; it stays as 'processing' and will
		// eventually be picked up again after a restart.
		return
	}

	w.mu.Lock()
	w.succeeded++
	w.mu.Unlock()
}

func (w *OutboxWorker) handleFailure(event *OutboxEvent, publishErr error) {
	newRetryCount := event.RetryCount + 1
	errMsg := publishErr.Error()

	if newRetryCount >= event.MaxRetries || newRetryCount >= w.config.MaxRetries {
		// Dead-letter: mark as permanently failed.
		_, _ = w.db.Exec(
			`UPDATE outbox_events
			 SET status = $1, retry_count = $2, error_message = $3, updated_at = NOW()
			 WHERE id = $4`,
			OutboxStatusFailed, newRetryCount, errMsg, event.ID,
		)

		w.mu.Lock()
		w.deadLettered++
		w.mu.Unlock()
		return
	}

	// Exponential backoff: base * 2^retryCount
	backoff := w.config.RetryBackoffBase * time.Duration(math.Pow(2, float64(newRetryCount-1)))
	nextRetry := time.Now().Add(backoff)

	_, _ = w.db.Exec(
		`UPDATE outbox_events
		 SET status = $1, retry_count = $2, next_retry_at = $3,
		     error_message = $4, updated_at = NOW()
		 WHERE id = $5`,
		OutboxStatusFailed, newRetryCount, nextRetry, errMsg, event.ID,
	)

	w.mu.Lock()
	w.failed++
	w.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func scanOutboxEvent(rows *sql.Rows) (*OutboxEvent, error) {
	var e OutboxEvent
	var aggregateID, aggregateType, errorMessage sql.NullString
	var nextRetryAt sql.NullTime

	err := rows.Scan(
		&e.ID,
		&e.EventType,
		&e.EventData,
		&aggregateID,
		&aggregateType,
		&e.OccurredAt,
		&e.Status,
		&e.RetryCount,
		&e.MaxRetries,
		&nextRetryAt,
		&errorMessage,
		&e.CreatedAt,
		&e.UpdatedAt,
		&e.Version,
	)
	if err != nil {
		return nil, fmt.Errorf("scan outbox event: %w", err)
	}

	if aggregateID.Valid {
		e.AggregateID = &aggregateID.String
	}
	if aggregateType.Valid {
		e.AggregateType = &aggregateType.String
	}
	if nextRetryAt.Valid {
		e.NextRetryAt = &nextRetryAt.Time
	}
	if errorMessage.Valid {
		e.ErrorMessage = &errorMessage.String
	}

	return &e, nil
}
