package outbox

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

// PostgreSQL repository implementation
type postgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository creates a new PostgreSQL repository
func NewPostgresRepository(db *sql.DB) Repository {
	return &postgresRepository{db: db}
}

// Store stores a new outbox event
func (r *postgresRepository) Store(event *Event) error {
	query := `
		INSERT INTO outbox_events (
			id, event_type, event_data, aggregate_id, aggregate_type,
			occurred_at, status, retry_count, max_retries, next_retry_at,
			error_message, created_at, updated_at, version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
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
			   error_message, created_at, updated_at, version
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
			   error_message, created_at, updated_at, version
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

// scanEvent scans a database row into an Event struct
func (r *postgresRepository) scanEvent(scanner interface{ Scan(...interface{}) error }) (*Event, error) {
	var event Event
	var aggregateID, aggregateType, nextRetryAt, errorMessage sql.NullString

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
	)

	if err != nil {
		return nil, fmt.Errorf("failed to scan event: %w", err)
	}

	if aggregateID.Valid {
		event.AggregateID = &aggregateID.String
	}

	if aggregateType.Valid {
		event.AggregateType = &aggregateType.String
	}

	if nextRetryAt.Valid {
		if t, err := time.Parse(time.RFC3339, nextRetryAt.String); err == nil {
			event.NextRetryAt = &t
		}
	}

	if errorMessage.Valid {
		event.ErrorMessage = &errorMessage.String
	}

	return &event, nil
}

// NewEvent creates a new outbox event
func NewEvent(eventType string, data interface{}, aggregateID, aggregateType *string) (*Event, error) {
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
	}, nil
}

