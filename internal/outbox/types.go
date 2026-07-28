package outbox

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event represents an outbox event
type Event struct {
	ID              uuid.UUID       `json:"id" db:"id"`
	TenantID        string          `json:"tenant_id" db:"tenant_id"`
	EventType       string          `json:"event_type" db:"event_type"`
	EventData       json.RawMessage `json:"event_data" db:"event_data"`
	AggregateID     *string         `json:"aggregate_id,omitempty" db:"aggregate_id"`
	AggregateType   *string         `json:"aggregate_type,omitempty" db:"aggregate_type"`
	OccurredAt      time.Time       `json:"occurred_at" db:"occurred_at"`
	Status          Status          `json:"status" db:"status"`
	RetryCount      int             `json:"retry_count" db:"retry_count"`
	MaxRetries      int             `json:"max_retries" db:"max_retries"`
	NextRetryAt     *time.Time      `json:"next_retry_at,omitempty" db:"next_retry_at"`
	ErrorMessage    *string         `json:"error_message,omitempty" db:"error_message"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
	Version         int             `json:"version" db:"version"`
	DeduplicationID *string         `json:"deduplication_id,omitempty" db:"deduplication_id"`
	TenantID        *string         `json:"tenant_id,omitempty" db:"tenant_id"`
	Partition       int             `json:"partition" db:"partition"`
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
	Type         string      `json:"type"`
	Data         interface{} `json:"data,omitempty"`
	Timestamp    time.Time   `json:"timestamp"`
	ID           string      `json:"id"`
	Encrypted    bool        `json:"encrypted,omitempty"`
	JWE          string      `json:"jwe,omitempty"`
	KeyID        string      `json:"key_id,omitempty"`
	SubscriberID string      `json:"subscriber_id,omitempty"`
}

// OutboxEvent is the public event type used by the router and publisher layers.
type OutboxEvent = Event

// Publisher interface for event publishing
type Publisher interface {
	Publish(ctx context.Context, event *Event) error
}

// Repository interface for outbox operations
type Repository interface {
	Store(ctx context.Context, event *Event) error
	GetPendingEvents(limit int) ([]*Event, error)
	GetByID(id uuid.UUID) (*Event, error)
	UpdateStatus(id uuid.UUID, status Status, errorMessage *string) error
	MarkAsProcessing(id uuid.UUID) error
	IncrementRetryCount(id uuid.UUID, nextRetryAt time.Time, errorMessage *string) error
	DeleteCompletedEvents(olderThan time.Time) (int64, error)
	ListDeadLetteredEvents(limit int) ([]*Event, error)
	RequeueEvent(id uuid.UUID) error
	// Publisher progress tracking (per-publisher high-water marks)
	EnsurePublisherProgressTable() error
	GetPublisherProgress(publisher string) (*uuid.UUID, error)
	GetPendingEventsForPublisher(publisher string, limit int) ([]*Event, error)
	MarkPublished(publisher string, event *Event, publishers []string) error
}

// ShardedRepository extends Repository with partition-aware queries used by
// the sharded dispatcher to process only events in owned partitions.
type ShardedRepository interface {
	Repository
	GetPendingEventsForShards(shards []int, limit int) ([]*Event, error)
}

// Dispatcher handles the outbox event dispatching
type Dispatcher interface {
	Start() error
	Stop() error
	IsRunning() bool
}
