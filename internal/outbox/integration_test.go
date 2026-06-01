package outbox

import (
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *sql.DB {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil || db.Ping() != nil {
		t.Skip("Postgres not available")
		return nil
	}
	return db
}

// Integration tests for edge cases and specific scenarios

func TestConcurrentEventPublishing(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	
	err := setupTestTable(db)
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
	
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
	
	for i := 0; i < 50; i++ {
		if len(publisher.GetPublishedEvents()) >= numGoroutines*eventsPerGoroutine {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	
	publishedEvents := publisher.GetPublishedEvents()
	require.Len(t, publishedEvents, numGoroutines*eventsPerGoroutine)
	
	pendingEvents, err := repo.GetPendingEvents(100)
	require.NoError(t, err)
	assert.Len(t, pendingEvents, 0)
}

func TestDuplicateEventHandling(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	
	err := setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	
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
	
	err = repo.Store(event)
	require.NoError(t, err)
	
	event2 := &Event{
		ID:        uuid.New(), 
		EventType: event.EventType,
		EventData: event.EventData,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Version:   1,
	}
	
	err = repo.Store(event2)
	require.NoError(t, err)
	
	retrieved1, err := repo.GetByID(event.ID)
	require.NoError(t, err)
	assert.Equal(t, event.ID, retrieved1.ID)
	
	retrieved2, err := repo.GetByID(event2.ID)
	require.NoError(t, err)
	assert.Equal(t, event2.ID, retrieved2.ID)
}

func TestStuckMessageRecovery(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	
	err := setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	publisher := NewMockPublisher()
	
	config := DefaultDispatcherConfig()
	config.PollInterval = 50 * time.Millisecond
	config.ProcessingTimeout = 500 * time.Millisecond
	config.MaxRetries = 2
	
	dispatcher := NewDispatcher(repo, publisher, config)
	
	stuckEvent, err := NewEvent("stuck.test", map[string]string{"key": "value"}, nil, nil)
	require.NoError(t, err)
	
	err = repo.Store(stuckEvent)
	require.NoError(t, err)
	
	publisher.SetPublishError(stuckEvent.ID, &TimeoutError{msg: "always fails"})
	
	err = dispatcher.Start()
	require.NoError(t, err)
	defer dispatcher.Stop()
	
	for i := 0; i < 50; i++ {
		_, _ = db.Exec("UPDATE outbox_events SET next_retry_at = NOW() - INTERVAL '1 minute' WHERE next_retry_at IS NOT NULL")
		time.Sleep(100 * time.Millisecond)
		retrieved, err := repo.GetByID(stuckEvent.ID)
		if err == nil && retrieved.Status == StatusFailed {
			break
		}
	}
	
	retrieved, err := repo.GetByID(stuckEvent.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, retrieved.Status)
	assert.GreaterOrEqual(t, retrieved.RetryCount, config.MaxRetries)
	
	// Remove the error and manually reset for recovery
	delete(publisher.publishErrors, stuckEvent.ID)
	err = repo.UpdateStatus(stuckEvent.ID, StatusPending, nil)
	require.NoError(t, err)
	
	// Wait for recovery
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		retrieved, err = repo.GetByID(stuckEvent.ID)
		if err == nil && retrieved.Status == StatusCompleted {
			break
		}
	}
	
	retrieved, err = repo.GetByID(stuckEvent.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, retrieved.Status)
}

func TestPartialFailureRecovery(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	
	err := setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	publisher := NewMockPublisher()
	
	config := DefaultDispatcherConfig()
	config.PollInterval = 50 * time.Millisecond
	config.BatchSize = 5
	
	dispatcher := NewDispatcher(repo, publisher, config)
	
	var events []*Event
	for i := 0; i < 5; i++ {
		event, err := NewEvent("partial.test", map[string]int{"index": i}, nil, nil)
		require.NoError(t, err)
		events = append(events, event)
		err = repo.Store(event)
		require.NoError(t, err)
	}
	
	publisher.SetPublishError(events[1].ID, &TimeoutError{msg: "event 1 fails"})
	publisher.SetPublishError(events[3].ID, &TimeoutError{msg: "event 3 fails"})
	
	err = dispatcher.Start()
	require.NoError(t, err)
	defer dispatcher.Stop()
	
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if len(publisher.GetPublishedEvents()) >= 3 {
			break
		}
	}
	
	publishedEvents := publisher.GetPublishedEvents()
	assert.True(t, len(publishedEvents) >= 2)
	
	// Remove errors for retry
	delete(publisher.publishErrors, events[1].ID)
	delete(publisher.publishErrors, events[3].ID)
	
	for i := 0; i < 50; i++ {
		_, _ = db.Exec("UPDATE outbox_events SET next_retry_at = NOW() - INTERVAL '1 minute' WHERE next_retry_at IS NOT NULL")
		time.Sleep(100 * time.Millisecond)
		if len(publisher.GetPublishedEvents()) >= 5 {
			break
		}
	}
	
	publishedEvents = publisher.GetPublishedEvents()
	require.Len(t, publishedEvents, 5)
	
	for _, event := range events {
		retrieved, err := repo.GetByID(event.ID)
		require.NoError(t, err)
		assert.Equal(t, StatusCompleted, retrieved.Status)
	}
}

func TestDatabaseConnectionFailure(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	
	err := setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	
	event, err := NewEvent("db.test", map[string]string{"key": "value"}, nil, nil)
	require.NoError(t, err)
	
	err = repo.Store(event)
	require.NoError(t, err)
	
	db.Close()
	
	_, err = repo.GetPendingEvents(10)
	assert.Error(t, err)
	
	err = repo.UpdateStatus(event.ID, StatusCompleted, nil)
	assert.Error(t, err)
}

func TestEventSerialization(t *testing.T) {
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
	
	var eventData EventData
	err = json.Unmarshal(event.EventData, &eventData)
	require.NoError(t, err)
	
	assert.Equal(t, "serialization.test", eventData.Type)
	actualData, _ := json.Marshal(eventData.Data)
	expectedData, _ := json.Marshal(complexData)
	assert.JSONEq(t, string(expectedData), string(actualData))
}

func TestBackoffStrategy(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	
	err := setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	publisher := NewMockPublisher()
	
	config := DefaultDispatcherConfig()
	config.PollInterval = 50 * time.Millisecond
	config.RetryBackoffFactor = 2.0
	config.MaxRetries = 3
	
	dispatcher := NewDispatcher(repo, publisher, config)
	
	event, err := NewEvent("backoff.test", map[string]string{"key": "value"}, nil, nil)
	require.NoError(t, err)
	
	err = repo.Store(event)
	require.NoError(t, err)
	
	publisher.SetPublishError(event.ID, &TimeoutError{msg: "always fails"})
	
	err = dispatcher.Start()
	require.NoError(t, err)
	defer dispatcher.Stop()
	
	var retryTimes []time.Time
	
	for i := 0; i < 3; i++ {
		time.Sleep(500 * time.Millisecond)
		retrieved, err := repo.GetByID(event.ID)
		require.NoError(t, err)
		if retrieved.RetryCount > len(retryTimes) {
			retryTimes = append(retryTimes, time.Now())
		}
	}
	
	if len(retryTimes) >= 2 {
		firstInterval := retryTimes[1].Sub(retryTimes[0])
		secondInterval := retryTimes[2].Sub(retryTimes[1])
		assert.True(t, secondInterval >= firstInterval, "Backoff should increase")
	}
}

func TestCleanupCompletedEvents(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		return
	}
	defer db.Close()
	
	err := setupTestTable(db)
	require.NoError(t, err)
	defer cleanupTestTable(db)
	
	repo := NewPostgresRepository(db)
	
	now := time.Now()
	for i := 0; i < 5; i++ {
		event, err := NewEvent("cleanup.test", map[string]int{"index": i}, nil, nil)
		require.NoError(t, err)
		
		if i < 2 {
			event.CreatedAt = now.Add(-25 * time.Hour) 
			event.UpdatedAt = now.Add(-25 * time.Hour)
		} else {
			event.CreatedAt = now.Add(-1 * time.Hour) 
			event.UpdatedAt = now.Add(-1 * time.Hour)
		}
		
		event.Status = StatusCompleted
		err = repo.Store(event)
		require.NoError(t, err)
	}
	
	cutoff := now.Add(-24 * time.Hour)
	deleted, err := repo.DeleteCompletedEvents(cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted) 
	
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM outbox_events").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func setupTestTable(db *sql.DB) error {
	_, _ = db.Exec("DROP TABLE IF EXISTS outbox_events")
	
	query := `
		CREATE TABLE IF NOT EXISTS outbox_events (
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
		
		CREATE INDEX IF NOT EXISTS idx_outbox_events_status ON outbox_events(status);
		CREATE INDEX IF NOT EXISTS idx_outbox_events_next_retry ON outbox_events(next_retry_at) WHERE next_retry_at IS NOT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_outbox_deduplication ON outbox_events(deduplication_id) WHERE deduplication_id IS NOT NULL;
	`
	
	_, err := db.Exec(query)
	return err
}

func cleanupTestTable(db *sql.DB) {
	_, _ = db.Exec("DROP TABLE IF EXISTS outbox_events")
}