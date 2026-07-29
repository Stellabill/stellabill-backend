package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"stellarbill-backend/internal/httpx"
)

// Service provides the main outbox functionality
type Service struct {
	repository Repository
	dispatcher Dispatcher
	db         *sql.DB
	jwe        *JWEConfig
	ring       *ConsistentHashRing
}

// JWEConfig holds optional JWE encryption settings for sensitive events.
type JWEConfig struct {
	Enabled   bool
	Keys      SubscriberKeyRepository
	Encryptor *JWEEncryptor
	Sensitive *SensitiveEventRegistry
}

// ServiceConfig holds configuration for the outbox service
type ServiceConfig struct {
	DispatcherConfig DispatcherConfig
	PublisherType    string // "console", "http", "multi"
	HTTPEndpoint     string
	JWE              *JWEConfig
	// HTTPPool is the shared connection pool used by the "http" and
	// "multi" publisher types. Defaults to a package-level shared pool
	// when nil, so callers only need to set it to share a pool across
	// multiple services or to override its tuning.
	HTTPPool *httpx.Pool
}

// NewService creates a new outbox service
func NewService(db *sql.DB, config ServiceConfig) (*Service, error) {
	repo := NewPostgresRepository(db)

	// Create publisher based on configuration
	var publisher Publisher
	httpClient := NewPooledHTTPClient(defaultHTTPPool)
	switch config.PublisherType {
	case "console":
		publisher = NewConsolePublisher()
	case "http":
		publisher = NewHTTPPublisher(config.HTTPEndpoint, httpClient)
	case "multi":
		publisher = NewMultiPublisher(
			NewConsolePublisher(),
			NewHTTPPublisher(config.HTTPEndpoint, httpClient),
		)
	default:
		publisher = NewConsolePublisher() // Default to console
	}

	if config.JWE != nil && config.JWE.Enabled && config.JWE.Keys != nil {
		encryptor := config.JWE.Encryptor
		if encryptor == nil {
			encryptor = NewJWEEncryptor()
		}
		sensitive := config.JWE.Sensitive
		if sensitive == nil {
			sensitive = NewSensitiveEventRegistry(nil)
		}
		publisher = NewJWEPublisher(publisher, config.JWE.Keys, encryptor, sensitive)
	}

	// Create dispatcher: use sharded dispatcher when shard config is set.
	var dispatcher Dispatcher
	var ring *ConsistentHashRing
	if config.DispatcherConfig.ShardCount > 0 {
		d, err := NewShardedDispatcher(repo, publisher, db, config.DispatcherConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create sharded dispatcher: %w", err)
		}
		dispatcher = d
		ring = NewConsistentHashRing(config.DispatcherConfig.ShardCount, 150)
	} else {
		dispatcher = NewDispatcher(repo, publisher, config.DispatcherConfig)
	}

	return &Service{
		repository: repo,
		dispatcher: dispatcher,
		db:         db,
		jwe:        config.JWE,
		ring:       ring,
	}, nil
}

// PublishEvent publishes an event using the outbox pattern
func (s *Service) PublishEvent(ctx context.Context, eventType string, data interface{}, aggregateID, aggregateType *string) error {
	tenantID := extractTenantID(ctx)

	event, err := s.buildEvent(eventType, data, aggregateID, aggregateType, nil, tenantID)
	if err != nil {
		return fmt.Errorf("failed to create event: %w", err)
	}

	// Compute partition from tenant_id using the consistent hash ring.
	if s.ring != nil && event.TenantID != nil {
		event.Partition = s.ring.GetPartition(*event.TenantID)
	}

	if err := s.storeEventInTransaction(ctx, event); err != nil {
		return fmt.Errorf("failed to store event: %w", err)
	}

	log.Printf("Event %s stored in outbox: %s", event.ID, eventType)
	return nil
}

func (s *Service) buildEvent(eventType string, data interface{}, aggregateID, aggregateType *string, deduplicationID *string, tenantID *string) (*Event, error) {
	subscriberID := ""
	if aggregateType != nil && aggregateID != nil && *aggregateType == "subscriber" {
		subscriberID = *aggregateID
	}
	if dataMap, ok := data.(map[string]interface{}); ok {
		if sid, ok := dataMap["subscriber_id"].(string); ok && sid != "" {
			subscriberID = sid
		} else if cid, ok := dataMap["customer_id"].(string); ok && cid != "" {
			subscriberID = cid
		}
	}

	var eventData json.RawMessage
	var err error
	if s.jwe != nil && s.jwe.Enabled && s.jwe.Keys != nil {
		encryptor := s.jwe.Encryptor
		if encryptor == nil {
			encryptor = NewJWEEncryptor()
		}
		sensitive := s.jwe.Sensitive
		if sensitive == nil {
			sensitive = NewSensitiveEventRegistry(nil)
		}
		eventData, err = PrepareEncryptedEventData(eventType, data, subscriberID, s.jwe.Keys, encryptor, sensitive)
	} else {
		event, createErr := NewEventWithDeduplication(eventType, data, aggregateID, aggregateType, deduplicationID)
		if event != nil {
			event.TenantID = tenantID
		}
		return event, createErr
	}
	if err != nil {
		if IsPermanentPublishError(err) {
			event := &Event{
				ID:              uuid.New(),
				EventType:       eventType,
				EventData:       mustMarshalEventData(eventType, data),
				AggregateID:     aggregateID,
				AggregateType:   aggregateType,
				OccurredAt:      time.Now(),
				Status:          StatusFailed,
				RetryCount:      0,
				MaxRetries:      3,
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
				Version:         1,
				DeduplicationID: deduplicationID,
				TenantID:        tenantID,
			}
			errMsg := err.Error()
			event.ErrorMessage = &errMsg
			return event, nil
		}
		return nil, err
	}

	return &Event{
		ID:              uuid.New(),
		EventType:       eventType,
		EventData:       eventData,
		AggregateID:     aggregateID,
		AggregateType:   aggregateType,
		OccurredAt:      time.Now(),
		Status:          StatusPending,
		RetryCount:      0,
		MaxRetries:      3,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
		Version:         1,
		DeduplicationID: deduplicationID,
		TenantID:        tenantID,
	}, nil
}

// storeEventInTransaction stores an event within a database transaction
func (s *Service) storeEventInTransaction(ctx context.Context, event *Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Store the event
	if err := s.repository.Store(ctx, event); err != nil {
		return fmt.Errorf("failed to store event in transaction: %w", err)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// PublishEventWithTx publishes an event within an existing transaction
func (s *Service) PublishEventWithTx(ctx context.Context, tx *sql.Tx, eventType string, data interface{}, aggregateID, aggregateType *string) (*Event, error) {
	event, err := s.buildEvent(eventType, data, aggregateID, aggregateType, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create event: %w", err)
	}

	if err := s.repository.Store(ctx, event); err != nil {
		return nil, fmt.Errorf("failed to store event: %w", err)
	}

	return event, nil
}

// Start starts the outbox dispatcher
func (s *Service) Start() error {
	return s.dispatcher.Start()
}

// Stop stops the outbox dispatcher
func (s *Service) Stop() error {
	return s.dispatcher.Stop()
}

// IsRunning returns whether the dispatcher is running
func (s *Service) IsRunning() bool {
	return s.dispatcher.IsRunning()
}

// GetEventStatus retrieves the status of a specific event
func (s *Service) GetEventStatus(id uuid.UUID) (*Event, error) {
	return s.repository.GetByID(id)
}

// GetPendingEventsCount returns the number of pending events (for monitoring)
func (s *Service) GetPendingEventsCount() (int, error) {
	events, err := s.repository.GetPendingEvents(1000) // Get up to 1000 events
	if err != nil {
		return 0, err
	}
	return len(events), nil
}

// Health check for the outbox service
func (s *Service) Health() error {
	// Check database connection
	if err := s.db.Ping(); err != nil {
		return fmt.Errorf("database health check failed: %w", err)
	}

	// Check dispatcher status
	if !s.dispatcher.IsRunning() {
		return fmt.Errorf("dispatcher is not running")
	}

	return nil
}

// OutboxManager provides a higher-level interface for managing outbox operations
type OutboxManager struct {
	service *Service
}

// NewOutboxManager creates a new outbox manager
func NewOutboxManager(service *Service) *OutboxManager {
	return &OutboxManager{service: service}
}

// PublishDomainEvent publishes a domain event
func (m *OutboxManager) PublishDomainEvent(ctx context.Context, domainEvent DomainEvent) error {
	return m.service.PublishEvent(ctx, domainEvent.EventType(), domainEvent.Data(), domainEvent.AggregateID(), domainEvent.AggregateType())
}

// DomainEvent interface for domain events
type DomainEvent interface {
	EventType() string
	Data() interface{}
	AggregateID() *string
	AggregateType() *string
	OccurredAt() time.Time
}

// Example domain event implementations

// SubscriptionCreated represents a subscription created event
type SubscriptionCreated struct {
	ID         string    `json:"id"`
	CustomerID string    `json:"customer_id"`
	PlanID     string    `json:"plan_id"`
	Status     string    `json:"status"`
	Timestamp  time.Time `json:"occurred_at"`
}

func (e SubscriptionCreated) EventType() string {
	return "subscription.created"
}

func (e SubscriptionCreated) Data() interface{} {
	return e
}

func (e SubscriptionCreated) AggregateID() *string {
	return &e.ID
}

func (e SubscriptionCreated) AggregateType() *string {
	aggregateType := "subscription"
	return &aggregateType
}

func (e SubscriptionCreated) OccurredAt() time.Time {
	return e.Timestamp
}

// PaymentProcessed represents a payment processed event
type PaymentProcessed struct {
	ID             string    `json:"id"`
	SubscriptionID string    `json:"subscription_id"`
	Amount         float64   `json:"amount"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
	Timestamp      time.Time `json:"occurred_at"`
}

func (e PaymentProcessed) EventType() string {
	return "payment.processed"
}

func (e PaymentProcessed) Data() interface{} {
	return e
}

func (e PaymentProcessed) AggregateID() *string {
	return &e.SubscriptionID
}

func (e PaymentProcessed) AggregateType() *string {
	aggregateType := "subscription"
	return &aggregateType
}

func (e PaymentProcessed) OccurredAt() time.Time {
	return e.Timestamp
}

func mustMarshalEventData(eventType string, data interface{}) json.RawMessage {
	envelope, err := NewEvent(eventType, data, nil, nil)
	if err != nil {
		raw, _ := json.Marshal(EventData{Type: eventType, Data: data, Timestamp: time.Now(), ID: uuid.New().String()})
		return raw
	}
	return envelope.EventData
}

// extractTenantID pulls the tenant_id value from the context. The middleware
// layer is expected to store it under the "tenant_id" key.
func extractTenantID(ctx context.Context) *string {
	if ctx == nil {
		return nil
	}
	if v := ctx.Value("tenant_id"); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return &s
		}
	}
	return nil
}
