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
)

// Integration tests for edge cases and specific scenarios

// TestConcurrentEventPublishing tests concurrent event publishing
func TestConcurrentEventPublishing(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://localhost/test_stellabill?sslmode=disable")
	require.NoError(t, err)
	defer db.Close()
	
	// Setup test table
	err = setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	publisher := NewMockPublisher()
	
	config := DefaultDispatcherConfig()
	config.PollInterval = 50 * time.Millisecond
	config.BatchSize = 10
	
	dispatcher := NewDispatcher(repo, publisher, config)
	
	err = dispatcher.Start()
	require.NoError(t, err)
	defer dispatcher.Stop()
	
	// Publish events concurrently
	numGoroutines := 10
	eventsPerGoroutine := 5
	
	done := make(chan bool, numGoroutines)
	
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			for j := 0; j < eventsPerGoroutine; j++ {
				event, err := NewEvent("concurrent.test", map[string]interface{}{
					"goroutine": goroutineID,
					"event":     j,
				}, nil, nil)
				require.NoError(t, err)
				
				err = repo.Store(event)
				require.NoError(t, err)
			}
			done <- true
		}(i)
	}
	
	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
	
	// Wait for processing
	time.Sleep(2 * time.Second)
	
	// Verify all events were processed
	publishedEvents := publisher.GetPublishedEvents()
	assert.Len(t, publishedEvents, numGoroutines*eventsPerGoroutine)
	
	// Verify no events are left pending
	pendingEvents, err := repo.GetPendingEvents(100)
	require.NoError(t, err)
	assert.Len(t, pendingEvents, 0)
}

// TestDuplicateEventHandling tests handling of duplicate events
func TestDuplicateEventHandling(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://localhost/test_stellabill?sslmode=disable")
	require.NoError(t, err)
	defer db.Close()
	
	err = setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	
	// Create an event
	eventID := uuid.New()
	event := &Event{
		ID:        eventID,
		EventType: "duplicate.test",
		EventData: []byte(`{"type":"duplicate.test","data":{"test":true},"timestamp":"2023-01-01T00:00:00Z","id":"test-id"}`),
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Version:   1,
	}
	
	// Store the event
	err = repo.Store(event)
	require.NoError(t, err)
	
	// Try to store the same event again (should succeed with different ID)
	event2 := &Event{
		ID:        uuid.New(), // Different ID
		EventType: event.EventType,
		EventData: event.EventData,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Version:   1,
	}
	
	err = repo.Store(event2)
	require.NoError(t, err)
	
	// Verify both events exist
	retrieved1, err := repo.GetByID(event.ID)
	require.NoError(t, err)
	assert.Equal(t, event.ID, retrieved1.ID)
	
	retrieved2, err := repo.GetByID(event2.ID)
	require.NoError(t, err)
	assert.Equal(t, event2.ID, retrieved2.ID)
}

// TestStuckMessageRecovery tests recovery from stuck messages
func TestStuckMessageRecovery(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://localhost/test_stellabill?sslmode=disable")
	require.NoError(t, err)
	defer db.Close()
	
	err = setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	publisher := NewMockPublisher()
	
	config := DefaultDispatcherConfig()
	config.PollInterval = 100 * time.Millisecond
	config.ProcessingTimeout = 500 * time.Millisecond
	config.MaxRetries = 2
	
	dispatcher := NewDispatcher(repo, publisher, config)
	
	// Create a stuck event (will always fail)
	stuckEvent, err := NewEvent("stuck.test", map[string]string{"key": "value"}, nil, nil)
	require.NoError(t, err)
	
	err = repo.Store(stuckEvent)
	require.NoError(t, err)
	
	// Set persistent error
	publisher.SetPublishError(stuckEvent.ID, &TimeoutError{msg: "always fails"})
	
	// Start dispatcher
	err = dispatcher.Start()
	require.NoError(t, err)
	defer dispatcher.Stop()
	
	// Wait for max retries
	time.Sleep(2 * time.Second)
	
	// Verify event is marked as failed
	retrieved, err := repo.GetByID(stuckEvent.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, retrieved.Status)
	assert.Equal(t, 2, retrieved.RetryCount) // Max retries reached
	
	// Remove the error and manually reset for recovery
	delete(publisher.publishErrors, stuckEvent.ID)
	err = repo.UpdateStatus(stuckEvent.ID, StatusPending, nil)
	require.NoError(t, err)
	
	// Wait for recovery
	time.Sleep(1 * time.Second)
	
	// Verify event was eventually processed
	retrieved, err = repo.GetByID(stuckEvent.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, retrieved.Status)
}

// TestPartialFailureRecovery tests recovery from partial failures
func TestPartialFailureRecovery(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://localhost/test_stellabill?sslmode=disable")
	require.NoError(t, err)
	defer db.Close()
	
	err = setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	publisher := NewMockPublisher()
	
	config := DefaultDispatcherConfig()
	config.PollInterval = 100 * time.Millisecond
	config.BatchSize = 5
	
	dispatcher := NewDispatcher(repo, publisher, config)
	
	// Create multiple events
	var events []*Event
	for i := 0; i < 5; i++ {
		event, err := NewEvent("partial.test", map[string]int{"index": i}, nil, nil)
		require.NoError(t, err)
		events = append(events, event)
		err = repo.Store(event)
		require.NoError(t, err)
	}
	
	// Set errors for some events
	publisher.SetPublishError(events[1].ID, &TimeoutError{msg: "event 1 fails"})
	publisher.SetPublishError(events[3].ID, &TimeoutError{msg: "event 3 fails"})
	
	// Start dispatcher
	err = dispatcher.Start()
	require.NoError(t, err)
	defer dispatcher.Stop()
	
	// Wait for initial processing
	time.Sleep(1 * time.Second)
	
	// Some events should have succeeded
	publishedEvents := publisher.GetPublishedEvents()
	assert.True(t, len(publishedEvents) >= 2) // At least events 0, 2, 4 should succeed
	
	// Remove errors for retry
	delete(publisher.publishErrors, events[1].ID)
	delete(publisher.publishErrors, events[3].ID)
	
	// Wait for retry
	time.Sleep(1 * time.Second)
	
	// All events should eventually succeed
	publishedEvents = publisher.GetPublishedEvents()
	assert.Len(t, publishedEvents, 5)
	
	// Verify all events are completed
	for _, event := range events {
		retrieved, err := repo.GetByID(event.ID)
		require.NoError(t, err)
		assert.Equal(t, StatusCompleted, retrieved.Status)
	}
}

// TestDatabaseConnectionFailure tests behavior when database connection fails
func TestDatabaseConnectionFailure(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://localhost/test_stellabill?sslmode=disable")
	require.NoError(t, err)
	defer db.Close()
	
	err = setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	
	// Create an event
	event, err := NewEvent("db.test", map[string]string{"key": "value"}, nil, nil)
	require.NoError(t, err)
	
	// Store event successfully
	err = repo.Store(event)
	require.NoError(t, err)
	
	// Close database connection to simulate failure
	db.Close()
	
	// Try to get pending events (should fail)
	_, err = repo.GetPendingEvents(10)
	assert.Error(t, err)
	
	// Try to update status (should fail)
	err = repo.UpdateStatus(event.ID, StatusCompleted, nil)
	assert.Error(t, err)
}

// TestEventSerialization tests event serialization/deserialization
func TestEventSerialization(t *testing.T) {
	// Test with complex data
	complexData := map[string]interface{}{
		"string":  "test",
		"number":  42,
		"boolean": true,
		"array":   []int{1, 2, 3},
		"object": map[string]interface{}{
			"nested": "value",
		},
	}
	
	event, err := NewEvent("serialization.test", complexData, nil, nil)
	require.NoError(t, err)
	
	// Verify event data can be unmarshaled
	var eventData EventData
	err = json.Unmarshal(event.EventData, &eventData)
	require.NoError(t, err)
	
	assert.Equal(t, "serialization.test", eventData.Type)
	assert.Equal(t, complexData, eventData.Data)
}

// TestBackoffStrategy tests exponential backoff
func TestBackoffStrategy(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://localhost/test_stellabill?sslmode=disable")
	require.NoError(t, err)
	defer db.Close()
	
	err = setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	publisher := NewMockPublisher()
	
	config := DefaultDispatcherConfig()
	config.PollInterval = 50 * time.Millisecond
	config.RetryBackoffFactor = 2.0
	config.MaxRetries = 3
	
	dispatcher := NewDispatcher(repo, publisher, config)
	
	// Create event that will fail
	event, err := NewEvent("backoff.test", map[string]string{"key": "value"}, nil, nil)
	require.NoError(t, err)
	
	err = repo.Store(event)
	require.NoError(t, err)
	
	// Set persistent error
	publisher.SetPublishError(event.ID, &TimeoutError{msg: "always fails"})
	
	// Start dispatcher
	err = dispatcher.Start()
	require.NoError(t, err)
	defer dispatcher.Stop()
	
	// Track retry times
	var retryTimes []time.Time
	startTime := time.Now()
	
	for i := 0; i < 3; i++ {
		time.Sleep(500 * time.Millisecond)
		retrieved, err := repo.GetByID(event.ID)
		require.NoError(t, err)
		if retrieved.RetryCount > len(retryTimes) {
			retryTimes = append(retryTimes, time.Now())
		}
	}
	
	// Verify exponential backoff (each retry should take longer)
	if len(retryTimes) >= 2 {
		firstInterval := retryTimes[1].Sub(retryTimes[0])
		secondInterval := retryTimes[2].Sub(retryTimes[1])
		assert.True(t, secondInterval >= firstInterval, "Backoff should increase")
	}
}

// TestCleanupCompletedEvents tests cleanup of old completed events
func TestCleanupCompletedEvents(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://localhost/test_stellabill?sslmode=disable")
	require.NoError(t, err)
	defer db.Close()
	
	err = setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	
	// Create completed events with different timestamps
	now := time.Now()
	for i := 0; i < 5; i++ {
		event, err := NewEvent("cleanup.test", map[string]int{"index": i}, nil, nil)
		require.NoError(t, err)
		
		// Set different creation times
		if i < 2 {
			event.CreatedAt = now.Add(-25 * time.Hour) // Old events
			event.UpdatedAt = now.Add(-25 * time.Hour)
		} else {
			event.CreatedAt = now.Add(-1 * time.Hour) // Recent events
			event.UpdatedAt = now.Add(-1 * time.Hour)
		}
		
		event.Status = StatusCompleted
		err = repo.Store(event)
		require.NoError(t, err)
	}
	
	// Delete events older than 24 hours
	cutoff := now.Add(-24 * time.Hour)
	deleted, err := repo.DeleteCompletedEvents(cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted) // Should delete 2 old events
	
	// Verify remaining events
	events, err := repo.GetPendingEvents(10)
	require.NoError(t, err)
	assert.Len(t, events, 3) // 3 recent events should remain
}

// Helper functions for integration tests
func setupTestTable(db *sql.DB) error {
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
	
	_, err := db.Exec(query)
	return err
}

func cleanupTestTable(db *sql.DB) {
	_, _ = db.Exec("DROP TABLE IF EXISTS outbox_events")
}
