package middleware

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrEventIDAlreadySeen = errors.New("event ID already seen")
)

// eventIDEntry represents a cached event ID with its expiration time.
type eventIDEntry struct {
	id        string
	expiresAt time.Time
}

// EventIDCache provides thread-safe storage for webhook event IDs
to prevent replay attacks. It automatically expires old entries after
a configurable TTL (time-to-live).
type EventIDCache struct {
	mu      sync.RWMutex
	entries map[string]time.Time
	ttl     time.Duration
}

// NewEventIDCache creates a new EventIDCache with the specified TTL.
// The TTL determines how long event IDs remain in the cache before
// being eligible for garbage collection. A TTL of 5-10 minutes is
// typically sufficient for webhook replay protection.
func NewEventIDCache(ttl time.Duration) *EventIDCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &EventIDCache{
		entries: make(map[string]time.Time),
		ttl:     ttl,
	}
}

// CheckAndStore checks if an event ID has been seen before, and if not,
// stores it in the cache with the configured TTL. This operation is atomic
// and thread-safe.
//
// Returns ErrEventIDAlreadySeen if the event ID is already in the cache.
// Returns nil if the event ID is new and has been stored successfully.
func (c *EventIDCache) CheckAndStore(ctx context.Context, eventID string) error {
	if eventID == "" {
		return errors.New("event ID cannot be empty")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Clean up expired entries before checking
	c.cleanupExpiredLocked()

	// Check if event ID already exists
	if expiry, exists := c.entries[eventID]; exists {
		if time.Now().Before(expiry) {
			return ErrEventIDAlreadySeen
		}
		// Entry has expired, remove it
		delete(c.entries, eventID)
	}

	// Store new event ID
	c.entries[eventID] = time.Now().Add(c.ttl)
	return nil
}

// Has checks if an event ID exists in the cache without storing it.
// Returns true if the event ID is found and hasn't expired.
func (c *EventIDCache) Has(ctx context.Context, eventID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	expiry, exists := c.entries[eventID]
	return exists && time.Now().Before(expiry)
}

// Remove deletes an event ID from the cache.
func (c *EventIDCache) Remove(ctx context.Context, eventID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, eventID)
}

// Len returns the number of non-expired event IDs in the cache.
func (c *EventIDCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.cleanupExpiredRLocked()
	return len(c.entries)
}

// Clear removes all entries from the cache.
func (c *EventIDCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]time.Time)
}

// cleanupExpiredLocked removes all expired entries from the cache.
// Caller must hold the write lock.
func (c *EventIDCache) cleanupExpiredLocked() {
	now := time.Now()
	for id, expiry := range c.entries {
		if now.After(expiry) {
			delete(c.entries, id)
		}
	}
}

// cleanupExpiredRLocked removes all expired entries from the cache.
// Caller must hold at least the read lock.
func (c *EventIDCache) cleanupExpiredRLocked() {
	// Upgrade to write lock for cleanup
	c.mu.RUnlock()
	c.mu.Lock()
	c.cleanupExpiredLocked()
	c.mu.Unlock()
	c.mu.RLock()
}
