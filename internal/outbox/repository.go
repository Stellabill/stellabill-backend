package outbox

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

var (
	ErrNilTransaction      = errors.New("transaction is nil")
	ErrEventNotDeadLettered = errors.New("event is not in dead-letter (failed) status")
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
			error_message, created_at, updated_at, version, dedupe_key
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
		event.DedupeKey,
	)

	if err != nil {
		return fmt.Errorf("failed to store outbox event: %w", err)
	}

	return nil
}

// StoreWithTx stores a new outbox event within the provided transaction.
// Returns ErrNilTransaction if tx is nil.
func (r *postgresRepository) StoreWithTx(tx *sql.Tx, event *Event) error {
	if tx == nil {
		return ErrNilTransaction
	}

	query := `
		INSERT INTO outbox_events (
			id, event_type, event_data, aggregate_id, aggregate_type,
			occurred_at, status, retry_count, max_retries, next_retry_at,
			error_message, created_at, updated_at, version, dedupe_key
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`

	_, err := tx.Exec(query,
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
		event.DedupeKey,
	)

	if err != nil {
		return fmt.Errorf("failed to store outbox event in transaction: %w", err)
	}

	return nil
}

// StoreIfNotExists inserts the event only when no row with the same dedupe_key
// already exists. Returns (true, nil) on insert, (false, nil) on duplicate.
func (r *postgresRepository) StoreIfNotExists(event *Event) (bool, error) {
	query := `
		INSERT INTO outbox_events (
			id, event_type, event_data, aggregate_id, aggregate_type,
			occurred_at, status, retry_count, max_retries, next_retry_at,
			error_message, created_at, updated_at, version, dedupe_key
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (dedupe_key) DO NOTHING
	`

	result, err := r.db.Exec(query,
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
		event.DedupeKey,
	)

	if err != nil {
		return false, fmt.Errorf("failed to store outbox event: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rows == 1, nil
}

// GetPendingEvents retrieves pending events for processing
func (r *postgresRepository) GetPendingEvents(limit int) ([]*Event, error) {
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, dedupe_key
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
			   error_message, created_at, updated_at, version, dedupe_key
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

// RecoverStuckEvents resets events stuck in processing status (updated_at < olderThan)
// back to pending, incrementing retry_count. Returns the number of events reset.
func (r *postgresRepository) RecoverStuckEvents(olderThan time.Time) (int64, error) {
	query := `
		UPDATE outbox_events
		SET status = 'pending', retry_count = retry_count + 1, updated_at = NOW()
		WHERE status = 'processing' AND updated_at < $1
	`

	result, err := r.db.Exec(query, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to recover stuck events: %w", err)
	}

	return result.RowsAffected()
}

// ListDeadLetterEvents returns events with status='failed' and retry_count >= max_retries,
// ordered by updated_at ASC, up to limit rows.
func (r *postgresRepository) ListDeadLetterEvents(limit int) ([]*Event, error) {
	query := `
		SELECT id, event_type, event_data, aggregate_id, aggregate_type,
			   occurred_at, status, retry_count, max_retries, next_retry_at,
			   error_message, created_at, updated_at, version, dedupe_key
		FROM outbox_events
		WHERE status = 'failed' AND retry_count >= max_retries
		ORDER BY updated_at ASC
		LIMIT $1
	`

	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list dead-letter events: %w", err)
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
		return nil, fmt.Errorf("error iterating dead-letter events: %w", err)
	}

	return events, nil
}

// RequeueEvent resets a dead-lettered event back to pending status with retry_count=0.
// Returns ErrEventNotDeadLettered if the event is not in failed status.
func (r *postgresRepository) RequeueEvent(id uuid.UUID) error {
	query := `
		UPDATE outbox_events
		SET status = 'pending', retry_count = 0, next_retry_at = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'failed'
	`

	result, err := r.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to requeue event: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return ErrEventNotDeadLettered
	}

	return nil
}

// scanEvent scans a database row into an Event struct
func (r *postgresRepository) scanEvent(scanner interface{ Scan(...interface{}) error }) (*Event, error) {
	var event Event
	var aggregateID, aggregateType, errorMessage sql.NullString
	var nextRetryAt sql.NullTime
	var dedupeKey sql.NullString

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
		&dedupeKey,
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
		event.NextRetryAt = &nextRetryAt.Time
	}

	if errorMessage.Valid {
		event.ErrorMessage = &errorMessage.String
	}

	if dedupeKey.Valid {
		event.DedupeKey = dedupeKey.String
	}

	return &event, nil
}

// computeDefaultDedupeKey generates a deterministic dedupe key from event_type,
// aggregate_id, and occurred_at using SHA-256.
func computeDefaultDedupeKey(eventType string, aggregateID *string, occurredAt time.Time) string {
	aggID := ""
	if aggregateID != nil {
		aggID = *aggregateID
	}
	input := fmt.Sprintf("%s:%s:%d", eventType, aggID, occurredAt.UnixNano())
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

// NewEvent creates a new outbox event.
// An optional explicit dedupeKey may be supplied as the fifth argument; if
// omitted (or empty), a default key is derived from eventType, aggregateID,
// and occurred_at via SHA-256.
func NewEvent(eventType string, data interface{}, aggregateID, aggregateType *string, dedupeKey ...string) (*Event, error) {
	// Sanitize PII fields before marshalling.
	sanitizedData, err := SanitizePayload(data, DefaultPIIFieldBlocklist)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}

	occurredAt := time.Now()

	eventData := EventData{
		Type:      eventType,
		Data:      json.RawMessage(sanitizedData),
		Timestamp: occurredAt,
		ID:        uuid.New().String(),
	}

	jsonData, err := json.Marshal(eventData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}

	// Determine dedupe key.
	key := computeDefaultDedupeKey(eventType, aggregateID, occurredAt)
	if len(dedupeKey) > 0 && dedupeKey[0] != "" {
		key = dedupeKey[0]
	}

	now := time.Now()
	return &Event{
		ID:            uuid.New(),
		EventType:     eventType,
		EventData:     json.RawMessage(jsonData),
		AggregateID:   aggregateID,
		AggregateType: aggregateType,
		OccurredAt:    occurredAt,
		Status:        StatusPending,
		RetryCount:    0,
		MaxRetries:    3,
		CreatedAt:     now,
		UpdatedAt:     now,
		Version:       1,
		DedupeKey:     key,
	}, nil
}
