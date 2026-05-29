package outbox

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event represents an outbox event
type Event struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	EventType     string     `json:"event_type" db:"event_type"`
	EventData     json.RawMessage `json:"event_data" db:"event_data"`
	AggregateID   *string    `json:"aggregate_id,omitempty" db:"aggregate_id"`
	AggregateType *string    `json:"aggregate_type,omitempty" db:"aggregate_type"`
	OccurredAt    time.Time  `json:"occurred_at" db:"occurred_at"`
	Status        Status     `json:"status" db:"status"`
	RetryCount    int        `json:"retry_count" db:"retry_count"`
	MaxRetries    int        `json:"max_retries" db:"max_retries"`
	NextRetryAt   *time.Time `json:"next_retry_at,omitempty" db:"next_retry_at"`
	ErrorMessage  *string    `json:"error_message,omitempty" db:"error_message"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
	Version       int        `json:"version" db:"version"`
	DeduplicationID *string  `json:"deduplication_id,omitempty" db:"deduplication_id"`
}

// Status represents the status of an outbox event
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

// EventData represents the structure of event data
type EventData struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
	ID        string      `json:"id"`
}

// Publisher interface for event publishing
type Publisher interface {
	Publish(event *Event) error
}

// Repository interface for outbox operations
type Repository interface {
	Store(event *Event) error
	GetPendingEvents(limit int) ([]*Event, error)
	GetByID(id uuid.UUID) (*Event, error)
	UpdateStatus(id uuid.UUID, status Status, errorMessage *string) error
	MarkAsProcessing(id uuid.UUID) error
	IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error
	DeleteCompletedEvents(olderThan time.Time) (int64, error)
}

// Dispatcher handles the outbox event dispatching
type Dispatcher interface {
	Start() error
	Stop() error
	IsRunning() bool
}
