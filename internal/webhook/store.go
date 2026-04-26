package webhook

import (
	"sync"
	"time"
)

// Event represents a webhook event with deduplication metadata
type Event struct {
	ProviderEventID string    // Unique ID from the webhook provider (e.g., Stripe event ID)
	TenantID        string    // Tenant ID to scope the event
	ProcessedAt     time.Time // When the event was processed
	EventType       string    // Type of the webhook event
}

// Store handles webhook event storage with deduplication
type Store struct {
	mu     sync.RWMutex
	events map[string]*Event // Key: tenantID:providerEventID
	ttl    time.Duration     // Time to live for events
}

// NewStore creates a new webhook event store
func NewStore(ttl time.Duration) *Store {
	s := &Store{
		events: make(map[string]*Event),
		ttl:    ttl,
	}
	// Start cleanup goroutine
	go s.cleanup()
	return s
}

// key generates a unique key for tenant+event ID combination
func (s *Store) key(tenantID, providerEventID string) string {
	return tenantID + ":" + providerEventID
}

// CheckAndStore checks if an event has been processed and stores it if not
// Returns true if the event was already processed (duplicate), false if it's new
func (s *Store) CheckAndStore(event *Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.key(event.TenantID, event.ProviderEventID)
	
	// Check if event already exists
	if existing, exists := s.events[key]; exists {
		// Event exists, check if it's within TTL
		if time.Since(existing.ProcessedAt) < s.ttl {
			return true // Duplicate
		}
		// Event expired, remove it
		delete(s.events, key)
	}
	
	// Store new event
	event.ProcessedAt = time.Now()
	s.events[key] = event
	return false // New event
}

// cleanup removes expired events periodically
func (s *Store) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for key, event := range s.events {
			if now.Sub(event.ProcessedAt) >= s.ttl {
				delete(s.events, key)
			}
		}
		s.mu.Unlock()
	}
}

// Count returns the number of stored events
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}

// Clear removes all events (useful for testing)
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = make(map[string]*Event)
}
