package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresPgxRepository implements Repository using pgx
type PostgresPgxRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresPgxRepository creates a new PostgresPgxRepository
func NewPostgresPgxRepository(pool *pgxpool.Pool) Repository {
	return &PostgresPgxRepository{pool: pool}
}

// Store stores a new outbox event
func (r *PostgresPgxRepository) Store(event *Event) error {
	ctx := context.Background()
	query := `
		INSERT INTO outbox_events (
			id, event_type, event_data, aggregate_id, aggregate_type,
			occurred_at, status, retry_count, max_retries, next_retry_at,
			error_message, created_at, updated_at, version, deduplication_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`

	_, err := r.pool.Exec(ctx, query,
		event.ID,
		event.EventType,
		event.EventData,
		event.AggregateID,
		event.AggregateType,
		event.OccurredAt,
		event.Status,
		event.RetryCount,
		event.MaxRetries,
		event.NextRetryAt,
		event.ErrorMessage,
		event.CreatedAt,
		event.UpdatedAt,
		event.Version,
		event.DeduplicationID,
	)
	if err != nil {
		return fmt.Errorf("failed to store outbox event: %w", err)
	}
	return nil
}

// GetPendingEvents retrieves pending events for processing
func (r *PostgresPgxRepository) GetPendingEvents(limit int) ([]*Event, error) {
	ctx := context.Background()
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM outbox_events
		WHERE status = $1 OR (status = $2 AND next_retry_at <= $3)
		ORDER BY occurred_at ASC
		LIMIT $4`

	rows, err := r.pool.Query(ctx, query, StatusPending, StatusFailed, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending events: %w", err)
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		event, err := r.scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pending events: %w", err)
	}
	return events, nil
}

// GetByID retrieves an event by ID
func (r *PostgresPgxRepository) GetByID(id uuid.UUID) (*Event, error) {
	ctx := context.Background()
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM outbox_events
		WHERE id = $1`

	row := r.pool.QueryRow(ctx, query, id)
	return r.scanEvent(row)
}

// UpdateStatus updates the status of an event
func (r *PostgresPgxRepository) UpdateStatus(id uuid.UUID, status Status, errorMessage *string) error {
	ctx := context.Background()
	query := `
		UPDATE outbox_events
		SET status = $1, error_message = $2, updated_at = $3
		WHERE id = $4`

	_, err := r.pool.Exec(ctx, query, status, errorMessage, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update event status: %w", err)
	}
	return nil
}

// MarkAsProcessing marks an event as being processed
func (r *PostgresPgxRepository) MarkAsProcessing(id uuid.UUID) error {
	ctx := context.Background()
	query := `
		UPDATE outbox_events
		SET status = $1, updated_at = $2
		WHERE id = $3 AND status = $4`

	result, err := r.pool.Exec(ctx, query, StatusProcessing, time.Now(), id, StatusPending)
	if err != nil {
		return fmt.Errorf("failed to mark event as processing: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("event not found or not in pending status")
	}
	return nil
}

// IncrementRetryCount increments the retry count and sets next retry time
func (r *PostgresPgxRepository) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error {
	ctx := context.Background()
	query := `
		UPDATE outbox_events
		SET retry_count = retry_count + 1, 
			next_retry_at = $1, 
			status = $2, 
			error_message = $3,
			updated_at = $4
		WHERE id = $5`

	_, err := r.pool.Exec(ctx, query, nextRetryAt, StatusFailed, errorMessage, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to increment retry count: %w", err)
	}
	return nil
}

// DeleteCompletedEvents deletes completed events older than the specified time
func (r *PostgresPgxRepository) DeleteCompletedEvents(olderThan time.Time) (int64, error) {
	ctx := context.Background()
	query := `
		DELETE FROM outbox_events
		WHERE status = $1 AND updated_at < $2`

	result, err := r.pool.Exec(ctx, query, StatusCompleted, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to delete completed events: %w", err)
	}
	return result.RowsAffected(), nil
}

// ListDeadLetteredEvents retrieves failed events (dead-letter queue).
func (r *PostgresPgxRepository) ListDeadLetteredEvents(limit int) ([]*Event, error) {
	ctx := context.Background()
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM dead_letter_events
		LIMIT $1`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list dead-lettered events: %w", err)
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		event, err := r.scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// RequeueEvent resets a failed event to pending for reprocessing.
func (r *PostgresPgxRepository) RequeueEvent(id uuid.UUID) error {
	ctx := context.Background()
	query := `
		UPDATE outbox_events
		SET status = $1, retry_count = 0, next_retry_at = NULL, error_message = NULL
		WHERE id = $2 AND status = $3`

	result, err := r.pool.Exec(ctx, query, StatusPending, id, StatusFailed)
	if err != nil {
		return fmt.Errorf("failed to requeue event: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("event not found or not in failed status")
	}
	return nil
}

// scanEvent scans a pgx row into an Event struct
func (r *PostgresPgxRepository) scanEvent(row pgx.Row) (*Event, error) {
	var event Event
	var aggregateID, aggregateType, errorMessage, deduplicationID sql.NullString
	var nextRetryAt sql.NullTime

	err := row.Scan(
		&event.ID,
		&event.EventType,
		&event.EventData,
		&aggregateID,
		&aggregateType,
		&event.OccurredAt,
		&event.Status,
		&event.RetryCount,
		&event.MaxRetries,
		&nextRetryAt,
		&errorMessage,
		&event.CreatedAt,
		&event.UpdatedAt,
		&event.Version,
		&deduplicationID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan event: %w", err)
	}

	if deduplicationID.Valid {
		event.DeduplicationID = &deduplicationID.String
	}
	if aggregateID.Valid {
		event.AggregateID = &aggregateID.String
	}
	if aggregateType.Valid {
		event.AggregateType = &aggregateType.String
	}
	if nextRetryAt.Valid {
		event.NextRetryAt = &nextRetryAt.Time
	}
	if errorMessage.Valid {
		event.ErrorMessage = &errorMessage.String
	}
	return &event, nil
}

// ListDeadLetteredEvents retrieves dead-lettered (failed) events
func (r *PostgresPgxRepository) ListDeadLetteredEvents(limit int) ([]*Event, error) {
	ctx := context.Background()
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM dead_letter_events
		LIMIT $1`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list dead-lettered events: %w", err)
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		event, err := r.scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating dead-lettered events: %w", err)
	}
	return events, nil
}

// RequeueEvent resets a failed event to pending for reprocessing
func (r *PostgresPgxRepository) RequeueEvent(id uuid.UUID) error {
	ctx := context.Background()
	query := `
		UPDATE outbox_events
		SET status = $1, retry_count = 0, next_retry_at = NULL, error_message = NULL
		WHERE id = $2 AND status = $3`

	result, err := r.pool.Exec(ctx, query, StatusPending, id, StatusFailed)
	if err != nil {
		return fmt.Errorf("failed to requeue event: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("event not found or not in failed status")
	}
	return nil
}
