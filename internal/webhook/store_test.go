package webhook

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStore_CheckAndStore_NewEvent(t *testing.T) {
	store := NewStore(24 * time.Hour)
	defer store.Clear()

	event := &Event{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_abc",
		EventType:       "payment.succeeded",
	}

	isDuplicate := store.CheckAndStore(event)
	assert.False(t, isDuplicate, "New event should not be marked as duplicate")
	assert.Equal(t, 1, store.Count(), "Store should have 1 event")
}

func TestStore_CheckAndStore_DuplicateEvent(t *testing.T) {
	store := NewStore(24 * time.Hour)
	defer store.Clear()

	event := &Event{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_abc",
		EventType:       "payment.succeeded",
	}

	// First call - new event
	isDuplicate := store.CheckAndStore(event)
	assert.False(t, isDuplicate, "First call should not be duplicate")

	// Second call - duplicate
	isDuplicate = store.CheckAndStore(event)
	assert.True(t, isDuplicate, "Second call should be marked as duplicate")
	assert.Equal(t, 1, store.Count(), "Store should still have 1 event")
}

func TestStore_CheckAndStore_TenantIsolation(t *testing.T) {
	store := NewStore(24 * time.Hour)
	defer store.Clear()

	// Same event ID, different tenants
	event1 := &Event{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_abc",
		EventType:       "payment.succeeded",
	}

	event2 := &Event{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_xyz",
		EventType:       "payment.succeeded",
	}

	// Both should be treated as different events
	isDuplicate1 := store.CheckAndStore(event1)
	assert.False(t, isDuplicate1, "First tenant event should not be duplicate")

	isDuplicate2 := store.CheckAndStore(event2)
	assert.False(t, isDuplicate2, "Second tenant event should not be duplicate")

	assert.Equal(t, 2, store.Count(), "Store should have 2 events (different tenants)")
}

func TestStore_CheckAndStore_TTLExpiration(t *testing.T) {
	store := NewStore(100 * time.Millisecond) // Short TTL for testing
	defer store.Clear()

	event := &Event{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_abc",
		EventType:       "payment.succeeded",
	}

	// First call - new event
	isDuplicate := store.CheckAndStore(event)
	assert.False(t, isDuplicate, "First call should not be duplicate")

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// After TTL, should be treated as new event
	isDuplicate = store.CheckAndStore(event)
	assert.False(t, isDuplicate, "After TTL expiration, should be treated as new event")
}

func TestStore_CheckAndStore_ConcurrentAccess(t *testing.T) {
	store := NewStore(24 * time.Hour)
	defer store.Clear()

	var wg sync.WaitGroup
	successCount := 0
	duplicateCount := 0
	mu := sync.Mutex{}

	// Launch 10 concurrent requests with the same event
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			event := &Event{
				ProviderEventID: "evt_123",
				TenantID:        "tenant_abc",
				EventType:       "payment.succeeded",
			}
			isDuplicate := store.CheckAndStore(event)
			
			mu.Lock()
			if isDuplicate {
				duplicateCount++
			} else {
				successCount++
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Only one should succeed, rest should be duplicates
	assert.Equal(t, 1, successCount, "Only one concurrent request should succeed")
	assert.Equal(t, 9, duplicateCount, "Rest should be marked as duplicates")
	assert.Equal(t, 1, store.Count(), "Store should have 1 event")
}

func TestStore_CheckAndStore_MultipleEvents(t *testing.T) {
	store := NewStore(24 * time.Hour)
	defer store.Clear()

	events := []*Event{
		{ProviderEventID: "evt_1", TenantID: "tenant_abc", EventType: "payment.succeeded"},
		{ProviderEventID: "evt_2", TenantID: "tenant_abc", EventType: "payment.failed"},
		{ProviderEventID: "evt_3", TenantID: "tenant_abc", EventType: "invoice.created"},
	}

	for _, event := range events {
		isDuplicate := store.CheckAndStore(event)
		assert.False(t, isDuplicate, "Each new event should not be duplicate")
	}

	assert.Equal(t, 3, store.Count(), "Store should have 3 events")
}

func TestStore_Cleanup(t *testing.T) {
	store := NewStore(100 * time.Millisecond) // Short TTL for testing
	defer store.Clear()

	// Add some events
	for i := 0; i < 5; i++ {
		event := &Event{
			ProviderEventID: "evt_" + string(rune(i)),
			TenantID:        "tenant_abc",
			EventType:       "test.event",
		}
		store.CheckAndStore(event)
	}

	assert.Equal(t, 5, store.Count(), "Store should have 5 events")

	// Wait for cleanup to run (cleanup runs every 5 minutes, but we trigger it manually by waiting)
	// Since we can't easily trigger the cleanup goroutine, we'll just verify the TTL logic works
	time.Sleep(150 * time.Millisecond)

	// Add a new event - this should trigger cleanup of expired events
	newEvent := &Event{
		ProviderEventID: "evt_new",
		TenantID:        "tenant_abc",
		EventType:       "test.event",
	}
	store.CheckAndStore(newEvent)

	// The old events should still be in the map (cleanup runs on ticker, not on every operation)
	// But we can verify that expired events are treated as new
	expiredEvent := &Event{
		ProviderEventID: "evt_0",
		TenantID:        "tenant_abc",
		EventType:       "test.event",
	}
	isDuplicate := store.CheckAndStore(expiredEvent)
	assert.False(t, isDuplicate, "Expired event should be treated as new")
}

func TestStore_Clear(t *testing.T) {
	store := NewStore(24 * time.Hour)

	// Add some events
	for i := 0; i < 3; i++ {
		event := &Event{
			ProviderEventID: "evt_" + string(rune(i)),
			TenantID:        "tenant_abc",
			EventType:       "test.event",
		}
		store.CheckAndStore(event)
	}

	assert.Equal(t, 3, store.Count(), "Store should have 3 events")

	store.Clear()
	assert.Equal(t, 0, store.Count(), "Store should be empty after clear")
}

func TestStore_KeyGeneration(t *testing.T) {
	store := NewStore(24 * time.Hour)
	defer store.Clear()

	// Test that key generation properly combines tenant and event ID
	event1 := &Event{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_abc",
		EventType:       "payment.succeeded",
	}

	event2 := &Event{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_abc",
		EventType:       "payment.succeeded",
	}

	event3 := &Event{
		ProviderEventID: "evt_456",
		TenantID:        "tenant_abc",
		EventType:       "payment.succeeded",
	}

	event4 := &Event{
		ProviderEventID: "evt_123",
		TenantID:        "tenant_xyz",
		EventType:       "payment.succeeded",
	}

	store.CheckAndStore(event1)
	assert.Equal(t, 1, store.Count())

	// Same key (tenant:event_id) - should be duplicate
	isDuplicate := store.CheckAndStore(event2)
	assert.True(t, isDuplicate)
	assert.Equal(t, 1, store.Count())

	// Different event ID - should be new
	isDuplicate = store.CheckAndStore(event3)
	assert.False(t, isDuplicate)
	assert.Equal(t, 2, store.Count())

	// Different tenant - should be new
	isDuplicate = store.CheckAndStore(event4)
	assert.False(t, isDuplicate)
	assert.Equal(t, 3, store.Count())
}
