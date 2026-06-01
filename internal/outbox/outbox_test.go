package outbox

import (
	"context"
	"database/sql"
	"os"
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
	db         *sql.DB
	repository Repository
	publisher  *MockPublisher
	dispatcher Dispatcher
	service    *Service
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

func (m *MockPublisher) Publish(ctx context.Context, event *Event) error {
	if delay, exists := m.delayedErrors[event.ID]; exists {
		time.Sleep(delay)
	}
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

func (suite *OutboxTestSuite) SetupSuite() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil || db.Ping() != nil {
		suite.T().Skip("Postgres not available")
		return
	}
	
	suite.db = db
	suite.repository = NewPostgresRepository(db)
	suite.publisher = NewMockPublisher()
	
	err = suite.createTestTables()
	require.NoError(suite.T(), err)
}

func (suite *OutboxTestSuite) TearDownSuite() {
	if suite.db != nil {
		suite.cleanupTestData()
		suite.db.Close()
	}
}

func (suite *OutboxTestSuite) SetupTest() {
	suite.cleanupTestData()
	suite.publisher.Reset()
	
	config := DefaultDispatcherConfig()
	config.PollInterval = 50 * time.Millisecond
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

func (suite *OutboxTestSuite) TearDownTest() {
	if suite.dispatcher != nil && suite.dispatcher.IsRunning() {
		_ = suite.dispatcher.Stop()
	}
	if suite.service != nil && suite.service.IsRunning() {
		_ = suite.service.Stop()
	}
}

func (suite *OutboxTestSuite) createTestTables() error {
	query := `
		DROP TABLE IF EXISTS outbox_events;
		CREATE TABLE outbox_events (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			event_type VARCHAR(255) NOT NULL,
			event_data JSONB NOT NULL,
			aggregate_id VARCHAR(255),
			aggregate_type VARCHAR(100),
			deduplication_id VARCHAR(255),
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
		CREATE INDEX idx_outbox_events_status ON outbox_events(status);
		CREATE INDEX idx_outbox_events_next_retry ON outbox_events(next_retry_at) WHERE next_retry_at IS NOT NULL;
		CREATE UNIQUE INDEX idx_outbox_deduplication ON outbox_events(deduplication_id) WHERE deduplication_id IS NOT NULL;
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

func (suite *OutboxTestSuite) TestRepositoryStoreAndGet() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	
	assert.Equal(suite.T(), event.ID, retrieved.ID)
	assert.Equal(suite.T(), event.EventType, retrieved.EventType)
	assert.Equal(suite.T(), event.Status, retrieved.Status)
}

func (suite *OutboxTestSuite) TestRepositoryGetPendingEvents() {
	for i := 0; i < 5; i++ {
		event, err := NewEvent("test.event", map[string]int{"index": i}, nil, nil)
		require.NoError(suite.T(), err)
		err = suite.repository.Store(event)
		require.NoError(suite.T(), err)
	}
	
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
	
	err = suite.repository.UpdateStatus(event.ID, StatusCompleted, nil)
	require.NoError(suite.T(), err)
	
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), StatusCompleted, retrieved.Status)
}

func (suite *OutboxTestSuite) TestRepositoryMarkAsProcessing() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	err = suite.repository.MarkAsProcessing(event.ID)
	require.NoError(suite.T(), err)
	
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
	
	err = suite.repository.IncrementRetryCount(event.ID, nextRetryAt, &errorMsg)
	require.NoError(suite.T(), err)
	
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), 1, retrieved.RetryCount)
	assert.Equal(suite.T(), StatusFailed, retrieved.Status)
	assert.Equal(suite.T(), &errorMsg, retrieved.ErrorMessage)
}

func (suite *OutboxTestSuite) TestConsolePublisher() {
	publisher := NewConsolePublisher()
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	
	err = publisher.Publish(context.Background(), event)
	assert.NoError(suite.T(), err)
}

func (suite *OutboxTestSuite) TestMockPublisher() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	
	err = suite.publisher.Publish(context.Background(), event)
	assert.NoError(suite.T(), err)
	assert.Len(suite.T(), suite.publisher.GetPublishedEvents(), 1)
	
	testError := &TimeoutError{msg: "timeout"}
	suite.publisher.SetPublishError(event.ID, testError)
	
	err = suite.publisher.Publish(context.Background(), event)
	assert.Error(suite.T(), err)
	assert.Equal(suite.T(), testError, err)
}

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
	for i := 0; i < 3; i++ {
		event, err := NewEvent("test.event", map[string]int{"index": i}, nil, nil)
		require.NoError(suite.T(), err)
		err = suite.repository.Store(event)
		require.NoError(suite.T(), err)
	}
	
	err := suite.dispatcher.Start()
	require.NoError(suite.T(), err)
	defer suite.dispatcher.Stop()
	
	for i := 0; i < 50; i++ {
		if len(suite.publisher.GetPublishedEvents()) >= 3 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	
	publishedEvents := suite.publisher.GetPublishedEvents()
	require.Len(suite.T(), publishedEvents, 3)
	
	for _, publishedEvent := range publishedEvents {
		retrieved, err := suite.repository.GetByID(publishedEvent.ID)
		require.NoError(suite.T(), err)
		assert.Equal(suite.T(), StatusCompleted, retrieved.Status)
	}
}

func (suite *OutboxTestSuite) TestDispatcherRetryMechanism() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	suite.publisher.SetPublishError(event.ID, &TimeoutError{msg: "timeout"})
	
	err = suite.dispatcher.Start()
	require.NoError(suite.T(), err)
	defer suite.dispatcher.Stop()
	
	time.Sleep(500 * time.Millisecond)
	
	// Remove error for second attempt
	delete(suite.publisher.publishErrors, event.ID)
	
	for i := 0; i < 50; i++ {
		suite.db.Exec("UPDATE outbox_events SET next_retry_at = NOW() - INTERVAL '1 minute' WHERE next_retry_at IS NOT NULL")
		if len(suite.publisher.GetPublishedEvents()) >= 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	
	publishedEvents := suite.publisher.GetPublishedEvents()
	require.Len(suite.T(), publishedEvents, 1)
	assert.Equal(suite.T(), event.ID, publishedEvents[0].ID)
	
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), StatusCompleted, retrieved.Status)
	assert.Greater(suite.T(), retrieved.RetryCount, 0)
}

func (suite *OutboxTestSuite) TestDispatcherMaxRetries() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	persistentError := &TimeoutError{msg: "persistent error"}
	suite.publisher.SetPublishError(event.ID, persistentError)
	
	err = suite.dispatcher.Start()
	require.NoError(suite.T(), err)
	defer suite.dispatcher.Stop()
	
	for i := 0; i < 50; i++ {
		suite.db.Exec("UPDATE outbox_events SET next_retry_at = NOW() - INTERVAL '1 minute' WHERE next_retry_at IS NOT NULL")
		time.Sleep(100 * time.Millisecond)
		retrieved, _ := suite.repository.GetByID(event.ID)
		if retrieved != nil && retrieved.Status == StatusFailed {
			break
		}
	}
	
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), StatusFailed, retrieved.Status)
	assert.GreaterOrEqual(suite.T(), retrieved.RetryCount, 3) 
	assert.NotNil(suite.T(), retrieved.ErrorMessage)
}

func (suite *OutboxTestSuite) TestServicePublishEvent() {
	err := suite.service.Start()
	require.NoError(suite.T(), err)
	defer suite.service.Stop()
	
	ctx := context.Background()
	err = suite.service.PublishEvent(ctx, "test.service.event", map[string]string{"service": "test"}, nil, nil)
	require.NoError(suite.T(), err)
	
	for i := 0; i < 50; i++ {
		pendingCount, _ := suite.service.GetPendingEventsCount()
		if pendingCount == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	
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

func (suite *OutboxTestSuite) TestCrashRecoveryPendingEvents() {
	for i := 0; i < 5; i++ {
		event, err := NewEvent("test.event", map[string]int{"index": i}, nil, nil)
		require.NoError(suite.T(), err)
		err = suite.repository.Store(event)
		require.NoError(suite.T(), err)
	}
	
	err := suite.dispatcher.Start()
	require.NoError(suite.T(), err)
	defer suite.dispatcher.Stop()
	
	for i := 0; i < 50; i++ {
		if len(suite.publisher.GetPublishedEvents()) >= 5 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	
	pendingEvents, err := suite.repository.GetPendingEvents(10)
	require.NoError(suite.T(), err)
	assert.Len(suite.T(), pendingEvents, 0)
	
	publishedEvents := suite.publisher.GetPublishedEvents()
	assert.Len(suite.T(), publishedEvents, 5)
}

func (suite *OutboxTestSuite) TestCrashRecoveryProcessingEvents() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	err = suite.repository.MarkAsProcessing(event.ID)
	require.NoError(suite.T(), err)
	
	_, err = suite.db.Exec("UPDATE outbox_events SET updated_at = NOW() - INTERVAL '1 hour' WHERE id = $1", event.ID)
	require.NoError(suite.T(), err)
	
	err = suite.dispatcher.Start()
	require.NoError(suite.T(), err)
	defer suite.dispatcher.Stop()
	
	for i := 0; i < 50; i++ {
		retrieved, _ := suite.repository.GetByID(event.ID)
		if retrieved != nil && retrieved.Status == StatusCompleted {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	
	retrieved, err := suite.repository.GetByID(event.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), StatusCompleted, retrieved.Status)
}

func (suite *OutboxTestSuite) TestIdempotency() {
	event, err := NewEvent("test.event", map[string]string{"key": "value"}, nil, nil)
	require.NoError(suite.T(), err)
	err = suite.repository.Store(event)
	require.NoError(suite.T(), err)
	
	err = suite.repository.MarkAsProcessing(event.ID)
	require.NoError(suite.T(), err)
	
	err = suite.repository.MarkAsProcessing(event.ID)
	assert.Error(suite.T(), err)
}

func TestOutboxTestSuite(t *testing.T) {
	suite.Run(t, new(OutboxTestSuite))
}

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
		err := publisher.Publish(context.Background(), event)
		if err != nil {
			b.Fatal(err)
		}
	}
}