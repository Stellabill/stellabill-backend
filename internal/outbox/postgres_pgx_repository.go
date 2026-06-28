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

func (r *PostgresPgxRepository) Store(event *Event) error {
	ctx := context.Background()
	query := `
		INSERT INTO outbox_events (
			id, event_type, event_data, aggregate_id, aggregate_type,
			occurred_at, status, retry_count, max_retries, next_retry_at,
			error_message, created_at, updated_at, version, deduplication_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`

	_, err := r.pool.Exec(ctx, query,
		event.ID, event.EventType, event.EventData, event.AggregateID, event.AggregateType,
		event.OccurredAt, event.Status, event.RetryCount, event.MaxRetries, event.NextRetryAt,
		event.ErrorMessage, event.CreatedAt, event.UpdatedAt, event.Version, event.DeduplicationID,
	)
	if err != nil {
		return fmt.Errorf("failed to store outbox event: %w", err)
	}
	return nil
}

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
	return events, rows.Err()
}

func (r *PostgresPgxRepository) GetByID(id uuid.UUID) (*Event, error) {
	ctx := context.Background()
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM outbox_events WHERE id = $1`
	return r.scanEvent(r.pool.QueryRow(ctx, query, id))
}

func (r *PostgresPgxRepository) UpdateStatus(id uuid.UUID, status Status, errorMessage *string) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx,
		`UPDATE outbox_events SET status=$1, error_message=$2, updated_at=$3 WHERE id=$4`,
		status, errorMessage, time.Now(), id)
	return err
}

func (r *PostgresPgxRepository) MarkAsProcessing(id uuid.UUID) error {
	ctx := context.Background()
	result, err := r.pool.Exec(ctx,
		`UPDATE outbox_events SET status=$1, updated_at=$2 WHERE id=$3 AND status=$4`,
		StatusProcessing, time.Now(), id, StatusPending)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("event not found or not in pending status")
	}
	return nil
}

func (r *PostgresPgxRepository) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx,
		`UPDATE outbox_events SET retry_count=retry_count+1, next_retry_at=$1, status=$2, error_message=$3, updated_at=$4 WHERE id=$5`,
		nextRetryAt, StatusFailed, errorMessage, time.Now(), id)
	return err
}

func (r *PostgresPgxRepository) DeleteCompletedEvents(olderThan time.Time) (int64, error) {
	ctx := context.Background()
	result, err := r.pool.Exec(ctx,
		`DELETE FROM outbox_events WHERE status=$1 AND updated_at<$2`, StatusCompleted, olderThan)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *PostgresPgxRepository) ListDeadLetteredEvents(limit int) ([]*Event, error) {
	ctx := context.Background()
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM dead_letter_events LIMIT $1`
	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
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

func (r *PostgresPgxRepository) RequeueEvent(id uuid.UUID) error {
	ctx := context.Background()
	result, err := r.pool.Exec(ctx,
		`UPDATE outbox_events SET status=$1, retry_count=0, next_retry_at=NULL, error_message=NULL, updated_at=$2 WHERE id=$3 AND status=$4`,
		StatusPending, time.Now(), id, StatusFailed)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("event not found or not in failed status")
	}
	return nil
}

func (r *PostgresPgxRepository) EnsurePublisherProgressTable() error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS outbox_publisher_progress (
			publisher TEXT PRIMARY KEY,
			last_processed_at TIMESTAMPTZ,
			last_processed_id UUID,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`)
	return err
}

func (r *PostgresPgxRepository) GetPublisherProgress(publisher string) (*time.Time, *uuid.UUID, error) {
	ctx := context.Background()
	var lastAt sql.NullTime
	var lastID uuid.NullUUID
	err := r.pool.QueryRow(ctx,
		`SELECT last_processed_at, last_processed_id FROM outbox_publisher_progress WHERE publisher=$1`,
		publisher).Scan(&lastAt, &lastID)
	if err == pgx.ErrNoRows {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var t *time.Time
	var id *uuid.UUID
	if lastAt.Valid {
		t = &lastAt.Time
	}
	if lastID.Valid {
		id = &lastID.UUID
	}
	return t, id, nil
}

func (r *PostgresPgxRepository) UpdatePublisherProgress(publisher string, lastProcessedAt time.Time, lastProcessedID uuid.UUID) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO outbox_publisher_progress (publisher, last_processed_at, last_processed_id, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (publisher) DO UPDATE SET
			last_processed_at=EXCLUDED.last_processed_at,
			last_processed_id=EXCLUDED.last_processed_id,
			updated_at=NOW()`,
		publisher, lastProcessedAt, lastProcessedID)
	return err
}

func (r *PostgresPgxRepository) GetPendingEventsSince(since *time.Time, lastID *uuid.UUID, limit int) ([]*Event, error) {
	ctx := context.Background()
	var rows pgx.Rows
	var err error
	if since != nil && lastID != nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, event_type, event_data, aggregate_id, aggregate_type,
				   occurred_at, status, retry_count, max_retries, next_retry_at,
				   error_message, created_at, updated_at, version, deduplication_id
			FROM outbox_events
			WHERE status=$1 AND (occurred_at > $2 OR (occurred_at = $2 AND id > $3))
			ORDER BY occurred_at ASC, id ASC LIMIT $4`,
			StatusPending, since, lastID, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, event_type, event_data, aggregate_id, aggregate_type,
				   occurred_at, status, retry_count, max_retries, next_retry_at,
				   error_message, created_at, updated_at, version, deduplication_id
			FROM outbox_events WHERE status=$1
			ORDER BY occurred_at ASC, id ASC LIMIT $2`,
			StatusPending, limit)
	}
	if err != nil {
		return nil, err
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

func (r *PostgresPgxRepository) scanEvent(row pgx.Row) (*Event, error) {
	var event Event
	var aggregateID, aggregateType, errorMessage, deduplicationID sql.NullString
	var nextRetryAt sql.NullTime

	err := row.Scan(
		&event.ID, &event.EventType, &event.EventData,
		&aggregateID, &aggregateType,
		&event.OccurredAt, &event.Status, &event.RetryCount, &event.MaxRetries,
		&nextRetryAt, &errorMessage,
		&event.CreatedAt, &event.UpdatedAt, &event.Version, &deduplicationID,
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

// EnsurePublisherProgressTable creates the publisher_progress table if it does not exist.
func (r *PostgresPgxRepository) EnsurePublisherProgressTable() error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS publisher_progress (
			publisher   TEXT PRIMARY KEY,
			last_processed_at TIMESTAMPTZ NOT NULL,
			last_processed_id UUID NOT NULL
		)`)
	return err
}

// GetPublisherProgress returns the last processed cursor for a publisher.
func (r *PostgresPgxRepository) GetPublisherProgress(publisher string) (*time.Time, *uuid.UUID, error) {
	ctx := context.Background()
	row := r.pool.QueryRow(ctx,
		`SELECT last_processed_at, last_processed_id FROM publisher_progress WHERE publisher = $1`,
		publisher)
	var t time.Time
	var id uuid.UUID
	if err := row.Scan(&t, &id); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return &t, &id, nil
}

// UpdatePublisherProgress upserts the publisher cursor.
func (r *PostgresPgxRepository) UpdatePublisherProgress(publisher string, lastProcessedAt time.Time, lastProcessedID uuid.UUID) error {
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO publisher_progress (publisher, last_processed_at, last_processed_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (publisher) DO UPDATE
		SET last_processed_at = EXCLUDED.last_processed_at,
		    last_processed_id  = EXCLUDED.last_processed_id`,
		publisher, lastProcessedAt, lastProcessedID)
	return err
}

// GetPendingEventsSince returns pending events after the given cursor.
func (r *PostgresPgxRepository) GetPendingEventsSince(since *time.Time, lastID *uuid.UUID, limit int) ([]*Event, error) {
	ctx := context.Background()
	var (
		rows pgx.Rows
		err  error
	)
	if since == nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			       occurred_at, status, retry_count, max_retries, next_retry_at,
			       error_message, created_at, updated_at, version, deduplication_id
			FROM outbox_events
			WHERE status = $1
			ORDER BY occurred_at ASC, id ASC
			LIMIT $2`, StatusPending, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			       occurred_at, status, retry_count, max_retries, next_retry_at,
			       error_message, created_at, updated_at, version, deduplication_id
			FROM outbox_events
			WHERE status = $1
			  AND (occurred_at > $2 OR (occurred_at = $2 AND id > $3))
			ORDER BY occurred_at ASC, id ASC
			LIMIT $4`, StatusPending, *since, lastID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pending events since: %w", err)
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		ev, err := r.scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}
