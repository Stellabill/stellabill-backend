package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// OutboxTestSuite provides a comprehensive test suite for the outbox pattern
type OutboxTestSuite struct {
	suite.Suite
	db          *sql.DB
	repository  Repository
	publisher   *MockPublisher
	dispatcher  Dispatcher
	service     *Service
}

// MockPublisher for testing
type MockPublisher struct {
	publishedEvents []*Event
	publishErrors   map[uuid.UUID]error
	delayedErrors   map[uuid.UUID]time.Duration
}

func NewMockPublisher() *MockPublisher {
	return &MockPublisher{
		publishedEvents: make([]*Event, 0),
		publishErrors:   make(map[uuid.UUID]error),
		delayedErrors:   make(map[uuid.UUID]time.Duration),
	}
}

func (m *MockPublisher) Publish(event *Event) error {
	// Check for delayed errors
	if delay, exists := m.delayedErrors[event.ID]; exists {
		time.Sleep(delay)
	}
	
	// Check for publish errors
	if err, exists := m.publishErrors[event.ID]; exists {
		return err
	}
	
	m.publishedEvents = append(m.publishedEvents, event)
	return nil
}

func (m *MockPublisher) SetPublishError(id uuid.UUID, err error) {
	m.publishErrors[id] = err
}

func (m *MockPublisher) SetDelayedError(id uuid.UUID, delay time.Duration) {
	m.delayedErrors[id] = delay
}

func (m *MockPublisher) GetPublishedEvents() []*Event {
	return m.publishedEvents
}

func (m *MockPublisher) Reset() {
	m.publishedEvents = make([]*Event, 0)
	m.publishErrors = make(map[uuid.UUID]error)
	m.delayedErrors = make(map[uuid.UUID]time.Duration)
}

// SetupSuite runs once before all tests
func (suite *OutboxTestSuite) SetupSuite() {
	// Use an in-memory PostgreSQL or test database
	// For this example, we'll use a test database connection string
	db, err := sql.Open("postgres", "postgres://localhost/test_stellabill?sslmode=disable")
	require.NoError(suite.T(), err)
	
	suite.db = db
	suite.repository = NewPostgresRepository(db)
	suite.publisher = NewMockPublisher()
	
	// Create test tables
	err = suite.createTestTables()
	require.NoError(suite.T(), err)
}

// TearDownSuite runs once after all tests
func (suite *OutboxTestSuite) TearDownSuite() {
	if suite.db != nil {
		suite.cleanupTestData()
		suite.db.Close()
	}
}

// SetupTest runs before each test
func (suite *OutboxTestSuite) SetupTest() {
	suite.cleanupTestData()
	suite.publisher.Reset()
	
	config := DefaultDispatcherConfig()
	config.PollInterval = 100 * time.Millisecond
	config.BatchSize = 5
	config.ProcessingTimeout = 1 * time.Second
	
	suite.dispatcher = NewDispatcher(suite.repository, suite.publisher, config)
	
	serviceConfig := ServiceConfig{
		DispatcherConfig: config,
		PublisherType:    "console",
	}
	
	var err error
	suite.service, err = NewService(suite.db, serviceConfig)
	require.NoError(suite.T(), err)
}

// TearDownTest runs after each test
func (suite *OutboxTestSuite) TearDownTest() {
	if suite.dispatcher != nil && suite.dispatcher.IsRunning() {
		err := suite.dispatcher.Stop()
		require.NoError(suite.T(), err)
	}
	
	if suite.service != nil && suite.service.IsRunning() {
		err := suite.service.Stop()
		require.NoError(suite.T(), err)
	}
}

func (suite *OutboxTestSuite) createTestTables() error {
	// Create the outbox table for testing
	query := `
		CREATE TABLE IF NOT EXISTS outbox_events (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			event_type VARCHAR(255) NOT NULL,
			event_data JSONB NOT NULL,
			aggregate_id VARCHAR(255),
			aggregate_type VARCHAR(100),
			occurred_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			status VARCHAR(50) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
			retry_count INTEGER NOT NULL DEFAULT 0,
			max_retries INTEGER NOT NULL DEFAULT 3,
			next_retry_at TIMESTAMP WITH TIME ZONE,
			error_message TEXT,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			version INTEGER NOT NULL DEFAULT 1
		);
		
		CREATE INDEX IF NOT EXISTS idx_outbox_events_status ON outbox_events(status);
		CREATE INDEX IF NOT EXISTS idx_outbox_events_next_retry ON outbox_events(next_retry_at) WHERE next_retry_at IS NOT NULL;
	`
	
	_, err := suite.db.Exec(query)
	return err
}

func (suite *OutboxTestSuite) cleanupTestData() {
	_, err := suite.db.Exec("DELETE FROM outbox_events")
	if err != nil {
		suite.T().Logf("Failed to cleanup test data: %v", err)
	}
}

// Test Event Creation
func (suite *OutboxTestSuite) TestNewEvent() {
	eventType := "test.event"
	data := map[string]interface{}{"key": "value"}
	aggregateID := "aggregate-123"
	aggregateType := "test-aggregate"
	
	event, err := NewEvent(eventType, data, &aggregateID, &aggregateType)
	
	require.NoError(suite.T(), err)
	assert.NotEqual(suite.T(), uuid.Nil, event.ID)
	assert.Equal(suite.T(), eventType, event.EventType)
	assert.Equal(suite.T(), StatusPending, event.Status)
	assert.Equal(suite.T(), 0, event.RetryCount)
	assert.Equal(suite.T(), 3, event.MaxRetries)
	assert.Equal(suite.T(), &aggregateID, event.AggregateID)
	assert.Equal(suite.T(), &aggregateType, event.AggregateType)
}

// Test Repository Operations
func (suite *OutboxTestSuite) TestRepositoryStoreAndGet() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	
	// Store event
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	// Get event by ID
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	
	assert.Equal(suite.T(), event.ID, retrieved.ID)
	assert.Equal(suite.T(), event.EventType, retrieved.EventType)
	assert.Equal(suite.T(), event.Status, retrieved.Status)
}

func (suite *OutboxTestSuite) TestRepositoryGetPendingEvents() {
	// Create multiple events
	for i := 0; i < 5; i++ {
		event, err := NewEvent("test.event", map[string]int{"index": i}, nil, nil)
		require.NoError(suite.T(), err)
		err = suite.repository.Store(event)
		require.NoError(suite.T(), err)
	}
	
	// Get pending events
	events, err := suite.repository.GetPendingEvents(3)
	require.NoError(suite.T(), err)
	assert.Len(suite.T(), events, 3)
	
	for _, event := range events {
		assert.Equal(suite.T(), StatusPending, event.Status)
	}
}

func (suite *OutboxTestSuite) TestRepositoryUpdateStatus() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	// Update status
	err = suite.repository.UpdateStatus(event.ID, StatusCompleted, nil)
	require.NoError(suite.T(), err)
	
	// Verify update
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), StatusCompleted, retrieved.Status)
}

func (suite *OutboxTestSuite) TestRepositoryMarkAsProcessing() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	// Mark as processing
	err = suite.repository.MarkAsProcessing(event.ID)
	require.NoError(suite.T(), err)
	
	// Verify update
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), StatusProcessing, retrieved.Status)
}

func (suite *OutboxTestSuite) TestRepositoryIncrementRetryCount() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	nextRetryAt := time.Now().Add(1 * time.Hour)
	errorMsg := "test error"
	
	// Increment retry count
	err = suite.repository.IncrementRetryCount(event.ID, nextRetryAt, &errorMsg)
	require.NoError(suite.T(), err)
	
	// Verify update
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), 1, retrieved.RetryCount)
	assert.Equal(suite.T(), StatusFailed, retrieved.Status)
	assert.Equal(suite.T(), &errorMsg, retrieved.ErrorMessage)
}

// Test Publisher Operations
func (suite *OutboxTestSuite) TestConsolePublisher() {
	publisher := NewConsolePublisher()
	
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	
	err = publisher.Publish(event)
	assert.NoError(suite.T(), err)
}

func (suite *OutboxTestSuite) TestMockPublisher() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	
	// Test successful publish
	err = suite.publisher.Publish(event)
	assert.NoError(suite.T(), err)
	assert.Len(suite.T(), suite.publisher.GetPublishedEvents(), 1)
	
	// Test publish error
	testError := &TimeoutError{msg: "timeout"}
	suite.publisher.SetPublishError(event.ID, testError)
	
	err = suite.publisher.Publish(event)
	assert.Error(suite.T(), err)
	assert.Equal(suite.T(), testError, err)
}

// Test Dispatcher Operations
func (suite *OutboxTestSuite) TestDispatcherStartStop() {
	assert.False(suite.T(), suite.dispatcher.IsRunning())
	
	err := suite.dispatcher.Start()
	require.NoError(suite.T(), err)
	assert.True(suite.T(), suite.dispatcher.IsRunning())
	
	err = suite.dispatcher.Stop()
	require.NoError(suite.T(), err)
	assert.False(suite.T(), suite.dispatcher.IsRunning())
}

func (suite *OutboxTestSuite) TestDispatcherProcessEvents() {
	// Create test events
	for i := 0; i < 3; i++ {
		event, err := NewEvent("test.event", map[string]int{"index": i}, nil, nil)
		require.NoError(suite.T(), err)
		err = suite.repository.Store(event)
		require.NoError(suite.T(), err)
	}
	
	// Start dispatcher
	err := suite.dispatcher.Start()
	require.NoError(suite.T(), err)
	defer suite.dispatcher.Stop()
	
	// Wait for processing
	time.Sleep(500 * time.Millisecond)
	
	// Verify events were published
	publishedEvents := suite.publisher.GetPublishedEvents()
	assert.Len(suite.T(), publishedEvents, 3)
	
	// Verify events are marked as completed
	for _, publishedEvent := range publishedEvents {
		retrieved, err := suite.repository.GetByID(publishedEvent.ID)
		require.NoError(suite.T(), err)
		assert.Equal(suite.T(), StatusCompleted, retrieved.Status)
	}
}

func (suite *OutboxTestSuite) TestDispatcherRetryMechanism() {
	// Create test event
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	// Set publish error for first attempt
	suite.publisher.SetPublishError(event.ID, &TimeoutError{msg: "timeout"})
	
	// Start dispatcher
	err = suite.dispatcher.Start()
	require.NoError(suite.T(), err)
	defer suite.dispatcher.Stop()
	
	// Wait for processing and retry
	time.Sleep(1 * time.Second)
	
	// Remove error for second attempt
	delete(suite.publisher.publishErrors, event.ID)
	
	// Wait for retry
	time.Sleep(1 * time.Second)
	
	// Verify event was eventually published
	publishedEvents := suite.publisher.GetPublishedEvents()
	assert.Len(suite.T(), publishedEvents, 1)
	assert.Equal(suite.T(), event.ID, publishedEvents[0].ID)
	
	// Verify event is marked as completed
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), StatusCompleted, retrieved.Status)
	assert.Greater(suite.T(), retrieved.RetryCount, 0)
}

func (suite *OutboxTestSuite) TestDispatcherMaxRetries() {
	// Create test event
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	// Set persistent publish error
	persistentError := &TimeoutError{msg: "persistent error"}
	suite.publisher.SetPublishError(event.ID, persistentError)
	
	// Start dispatcher
	err = suite.dispatcher.Start()
	require.NoError(suite.T(), err)
	defer suite.dispatcher.Stop()
	
	// Wait for max retries
	time.Sleep(2 * time.Second)
	
	// Verify event is marked as failed
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), StatusFailed, retrieved.Status)
	assert.Equal(suite.T(), 3, retrieved.RetryCount) // Max retries
	assert.NotNil(suite.T(), retrieved.ErrorMessage)
}

// Test Service Operations
func (suite *OutboxTestSuite) TestServicePublishEvent() {
	err := suite.service.Start()
	require.NoError(suite.T(), err)
	defer suite.service.Stop()
	
	ctx := context.Background()
	eventType := "test.service.event"
	data := map[string]string{"service": "test"}
	
	err = suite.service.PublishEvent(ctx, eventType, data, nil, nil)
	require.NoError(suite.T(), err)
	
	// Wait for processing
	time.Sleep(500 * time.Millisecond)
	
	// Verify event was processed
	pendingCount, err := suite.service.GetPendingEventsCount()
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), 0, pendingCount)
}

func (suite *OutboxTestSuite) TestServiceHealth() {
	err := suite.service.Start()
	require.NoError(suite.T(), err)
	defer suite.service.Stop()
	
	err = suite.service.Health()
	assert.NoError(suite.T(), err)
}

// Test Crash Recovery Scenarios
func (suite *OutboxTestSuite) TestCrashRecoveryPendingEvents() {
	// Create events that are in pending state
	for i := 0; i < 5; i++ {
		event, err := NewEvent("test.event", map[string]int{"index": i}, nil, nil)
		require.NoError(suite.T(), err)
		err = suite.repository.Store(event)
		require.NoError(suite.T(), err)
	}
	
	// Simulate crash by not starting dispatcher initially
	
	// Start dispatcher (simulating recovery)
	err := suite.dispatcher.Start()
	require.NoError(suite.T(), err)
	defer suite.dispatcher.Stop()
	
	// Wait for recovery processing
	time.Sleep(500 * time.Millisecond)
	
	// Verify all events were processed
	pendingEvents, err := suite.repository.GetPendingEvents(10)
	require.NoError(suite.T(), err)
	assert.Len(suite.T(), pendingEvents, 0)
	
	publishedEvents := suite.publisher.GetPublishedEvents()
	assert.Len(suite.T(), publishedEvents, 5)
}

func (suite *OutboxTestSuite) TestCrashRecoveryProcessingEvents() {
	// Create an event and mark it as processing (simulating crash during processing)
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	err = suite.repository.MarkAsProcessing(event.ID)
	require.NoError(suite.T(), err)
	
	// Wait for processing timeout
	time.Sleep(2 * time.Second)
	
	// The dispatcher should eventually retry processing the event
	err = suite.dispatcher.Start()
	require.NoError(suite.T(), err)
	defer suite.dispatcher.Stop()
	
	time.Sleep(500 * time.Millisecond)
	
	// Verify event was eventually processed
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), StatusCompleted, retrieved.Status)
}

// Test Idempotency
func (suite *OutboxTestSuite) TestIdempotency() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	
	// Try to mark as processing multiple times
	err = suite.repository.MarkAsProcessing(event.ID)
	require.NoError(suite.T(), err)
	
	// Second attempt should fail
	err = suite.repository.MarkAsProcessing(event.ID)
	assert.Error(suite.T(), err)
}

// Test Clean Tests
func TestOutboxTestSuite(t *testing.T) {
	suite.Run(t, new(OutboxTestSuite))
}

// Benchmark Tests
func BenchmarkNewEvent(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublisherPublish(b *testing.B) {
	publisher := NewConsolePublisher()
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := publisher.Publish(event)
		if err != nil {
			b.Fatal(err)
		}
	}
}
