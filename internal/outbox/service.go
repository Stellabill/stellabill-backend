package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"stellarbill-backend/internal/db"
)

// Service provides the main outbox functionality
type Service struct {
	repository Repository
	dispatcher Dispatcher
	db         *sql.DB
}

// ServiceConfig holds configuration for the outbox service
type ServiceConfig struct {
	DispatcherConfig DispatcherConfig
	PublisherType    string // "console", "http", "multi"
	HTTPEndpoint     string
}

// NewService creates a new outbox service
func NewService(db *sql.DB, config ServiceConfig) (*Service, error) {
	repo := NewPostgresRepository(db)
	
	// Create publisher based on configuration
	var publisher Publisher
	switch config.PublisherType {
	case "console":
		publisher = NewConsolePublisher()
	case "http":
		publisher = NewHTTPPublisher(config.HTTPEndpoint, &DefaultHTTPClient{})
	case "multi":
		publisher = NewMultiPublisher(
			NewConsolePublisher(),
			NewHTTPPublisher(config.HTTPEndpoint, &DefaultHTTPClient{}),
		)
	default:
		publisher = NewConsolePublisher() // Default to console
	}
	
	dispatcher := NewDispatcher(repo, publisher, config.DispatcherConfig)
	
	return &Service{
		repository: repo,
		dispatcher: dispatcher,
		db:         db,
	}, nil
}

// PublishEvent publishes an event using the outbox pattern
func (s *Service) PublishEvent(ctx context.Context, eventType string, data interface{}, aggregateID, aggregateType *string) error {
	return db.RunInTransaction(ctx, s.db, func(tx *sql.Tx) error {
		_, err := s.PublishEventWithTx(tx, eventType, data, aggregateID, aggregateType, nil)
		return err
	})
}



// PublishEventWithTx publishes an event within an existing transaction
func (s *Service) PublishEventWithTx(tx *sql.Tx, eventType string, data interface{}, aggregateID, aggregateType *string, deduplicationID *string) (*Event, error) {
	// Create the event
	event, err := NewEventWithDeduplication(eventType, data, aggregateID, aggregateType, deduplicationID)
	if err != nil {
		return nil, fmt.Errorf("failed to create event: %w", err)
	}
	
	// Store the event using the transaction-aware repository wrapper
	if err := NewPostgresRepository(tx).Store(event); err != nil {
		return nil, fmt.Errorf("failed to store event in transaction: %w", err)
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
	ID           string    `json:"id"`
	CustomerID   string    `json:"customer_id"`
	PlanID       string    `json:"plan_id"`
	Status       string    `json:"status"`
	Occurred     time.Time `json:"occurred_at"`
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
	return e.Occurred
}

// PaymentProcessed represents a payment processed event
type PaymentProcessed struct {
	ID           string    `json:"id"`
	SubscriptionID string   `json:"subscription_id"`
	Amount       float64   `json:"amount"`
	Currency     string    `json:"currency"`
	Status       string    `json:"status"`
	Occurred     time.Time `json:"occurred_at"`
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
	return e.Occurred
}
