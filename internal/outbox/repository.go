package outbox

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"stellarbill-backend/internal/db"
)

// PostgreSQL repository implementation
type postgresRepository struct {
	db db.DBTX
}

// NewPostgresRepository creates a new PostgreSQL repository
func NewPostgresRepository(executor db.DBTX) Repository {
	return &postgresRepository{db: executor}
}

// Store stores a new outbox event
func (r *postgresRepository) Store(event *Event) error {
	query := `
		INSERT INTO outbox_events (
			id, event_type, event_data, aggregate_id, aggregate_type,
			occurred_at, status, retry_count, max_retries, next_retry_at,
			error_message, created_at, updated_at, version, deduplication_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	
	_, err := r.db.Exec(query,
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
func (r *postgresRepository) GetPendingEvents(limit int) ([]*Event, error) {
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM outbox_events
		WHERE status = $1 OR (status = $2 AND next_retry_at <= $3)
		ORDER BY occurred_at ASC
		LIMIT $4
	`
	
	rows, err := r.db.Query(query, StatusPending, StatusFailed, time.Now(), limit)
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
func (r *postgresRepository) GetByID(id uuid.UUID) (*Event, error) {
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM outbox_events
		WHERE id = $1
	`
	
	row := r.db.QueryRow(query, id)
	return r.scanEvent(row)
}

// UpdateStatus updates the status of an event
func (r *postgresRepository) UpdateStatus(id uuid.UUID, status Status, errorMessage *string) error {
	query := `
		UPDATE outbox_events
		SET status = $1, error_message = $2, updated_at = $3
		WHERE id = $4
	`
	
	_, err := r.db.Exec(query, status, errorMessage, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update event status: %w", err)
	}
	
	return nil
}

// MarkAsProcessing marks an event as being processed
func (r *postgresRepository) MarkAsProcessing(id uuid.UUID) error {
	query := `
		UPDATE outbox_events
		SET status = $1, updated_at = $2
		WHERE id = $3 AND status = $4
	`
	
	result, err := r.db.Exec(query, StatusProcessing, time.Now(), id, StatusPending)
	if err != nil {
		return fmt.Errorf("failed to mark event as processing: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return fmt.Errorf("event not found or not in pending status")
	}
	
	return nil
}

// IncrementRetryCount increments the retry count and sets next retry time
func (r *postgresRepository) IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error {
	query := `
		UPDATE outbox_events
		SET retry_count = retry_count + 1, 
			next_retry_at = $1, 
			status = $2, 
			error_message = $3,
			updated_at = $4
		WHERE id = $5
	`
	
	_, err := r.db.Exec(query, nextRetryAt, StatusFailed, errorMessage, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to increment retry count: %w", err)
	}
	
	return nil
}

// DeleteCompletedEvents deletes completed events older than the specified time
func (r *postgresRepository) DeleteCompletedEvents(olderThan time.Time) (int64, error) {
	query := `
		DELETE FROM outbox_events
		WHERE status = $1 AND updated_at < $2
	`
	
	result, err := r.db.Exec(query, StatusCompleted, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to delete completed events: %w", err)
	}
	
	return result.RowsAffected()
}

// EnsurePublisherProgressTable ensures the publisher progress table exists
func (r *postgresRepository) EnsurePublisherProgressTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS outbox_publisher_progress (
		publisher VARCHAR(255) PRIMARY KEY,
		last_processed_at TIMESTAMPTZ,
		last_processed_id UUID,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	`

	if _, err := r.db.Exec(query); err != nil {
		return fmt.Errorf("failed to ensure publisher progress table: %w", err)
	}
	return nil
}

// GetPublisherProgress returns the last processed cursor for a publisher
func (r *postgresRepository) GetPublisherProgress(publisher string) (*time.Time, *uuid.UUID, error) {
	query := `SELECT last_processed_at, last_processed_id FROM outbox_publisher_progress WHERE publisher = $1`
	row := r.db.QueryRow(query, publisher)
	var lastAt sql.NullTime
	var lastID sql.NullString
	if err := row.Scan(&lastAt, &lastID); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to get publisher progress: %w", err)
	}

	var t *time.Time
	var id *uuid.UUID
	if lastAt.Valid {
		tmp := lastAt.Time
		t = &tmp
	}
	if lastID.Valid {
		parsed, err := uuid.Parse(lastID.String)
		if err == nil {
			id = &parsed
		}
	}
	return t, id, nil
}

// UpdatePublisherProgress sets or updates the publisher cursor
func (r *postgresRepository) UpdatePublisherProgress(publisher string, lastProcessedAt time.Time, lastProcessedID uuid.UUID) error {
	query := `
	INSERT INTO outbox_publisher_progress (publisher, last_processed_at, last_processed_id, updated_at)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (publisher) DO UPDATE SET last_processed_at = EXCLUDED.last_processed_at, last_processed_id = EXCLUDED.last_processed_id, updated_at = EXCLUDED.updated_at
	`
	if _, err := r.db.Exec(query, publisher, lastProcessedAt, lastProcessedID, time.Now()); err != nil {
		return fmt.Errorf("failed to update publisher progress: %w", err)
	}
	return nil
}

// GetPendingEventsSince returns pending events since the given cursor (occured_at and id)
func (r *postgresRepository) GetPendingEventsSince(since *time.Time, lastID *uuid.UUID, limit int) ([]*Event, error) {
	// Build query depending on whether since/lastID are provided
	var query string
	var args []interface{}
	if since == nil {
		query = `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM outbox_events
		WHERE status = $1 OR (status = $2 AND next_retry_at <= $3)
		ORDER BY occurred_at ASC, id ASC
		LIMIT $4`
		args = []interface{}{StatusPending, StatusFailed, time.Now(), limit}
	} else if lastID == nil {
		query = `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM outbox_events
		WHERE (status = $1 OR (status = $2 AND next_retry_at <= $3))
		  AND occurred_at >= $4
		ORDER BY occurred_at ASC, id ASC
		LIMIT $5`
		args = []interface{}{StatusPending, StatusFailed, time.Now(), *since, limit}
	} else {
		query = `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM outbox_events
		WHERE (status = $1 OR (status = $2 AND next_retry_at <= $3))
		  AND (occurred_at > $4 OR (occurred_at = $4 AND id > $5))
		ORDER BY occurred_at ASC, id ASC
		LIMIT $6`
		args = []interface{}{StatusPending, StatusFailed, time.Now(), *since, *lastID, limit}
	}

	rows, err := r.db.Query(query, args...)
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
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pending events since: %w", err)
	}
	return events, nil
}

// ListDeadLetteredEvents retrieves dead-lettered (failed) events
func (r *postgresRepository) ListDeadLetteredEvents(limit int) ([]*Event, error) {
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, deduplication_id
		FROM dead_letter_events
		LIMIT $1
	`
	
	rows, err := r.db.Query(query, limit)
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
func (r *postgresRepository) RequeueEvent(id uuid.UUID) error {
	query := `
		UPDATE outbox_events
		SET status = $1, retry_count = 0, next_retry_at = NULL, error_message = NULL
		WHERE id = $2 AND status = $3
	`
	
	result, err := r.db.Exec(query, StatusPending, id, StatusFailed)
	if err != nil {
		return fmt.Errorf("failed to requeue event: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return fmt.Errorf("event not found or not in failed status")
	}
	
	return nil
}

// scanEvent scans a database row into an Event struct
func (r *postgresRepository) scanEvent(scanner interface{ Scan(...interface{}) error }) (*Event, error) {
	var event Event
	var aggregateID, aggregateType, errorMessage, deduplicationID sql.NullString
	var nextRetryAt sql.NullTime
	
	err := scanner.Scan(
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

// NewEvent creates a new outbox event
func NewEvent(eventType string, data interface{}, aggregateID, aggregateType *string) (*Event, error) {
	return NewEventWithDeduplication(eventType, data, aggregateID, aggregateType, nil)
}

// NewEventWithDeduplication creates a new outbox event with an optional deduplication ID
func NewEventWithDeduplication(eventType string, data interface{}, aggregateID, aggregateType *string, deduplicationID *string) (*Event, error) {
	eventData := EventData{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
		ID:        uuid.New().String(),
	}
	
	jsonData, err := json.Marshal(eventData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}
	
	return &Event{
		ID:            uuid.New(),
		EventType:     eventType,
		EventData:     json.RawMessage(jsonData),
		AggregateID:   aggregateID,
		AggregateType: aggregateType,
		OccurredAt:    time.Now(),
		Status:        StatusPending,
		RetryCount:    0,
		MaxRetries:    3,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Version:       1,
		DeduplicationID: deduplicationID,
	}, nil
}
