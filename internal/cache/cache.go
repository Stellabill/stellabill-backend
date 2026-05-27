package cache

import (
	"context"
	"sync"
	"time"
)

// Cache is a tiny abstraction used for read caching.
type Cache interface {
	// Get loads the value for key. If not found, return (nil, nil).
	Get(ctx context.Context, key string) ([]byte, error)
	// Set stores value with TTL.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete removes a key.
	Delete(ctx context.Context, key string) error
}

// Flushable is an optional extension of Cache that supports bulk eviction.
// InMemory implements this interface; external caches (Redis, Memcached) may
// also implement it. Callers should type-assert to this before calling Flush.
type Flushable interface {
	Cache
	// Flush removes all entries and returns the count that were present.
	// It is safe to call concurrently and on an already-empty cache (idempotent).
	Flush(ctx context.Context) (keysEvicted int, err error)
	// Len returns the current number of entries (including not-yet-evicted expired ones).
	Len() int
}

// InMemory is a simple, thread-safe in-memory cache used for tests and default runs.
// It implements both Cache and Flushable.
type InMemory struct {
	mu    sync.RWMutex
	items map[string]inmemoryItem
}

type inmemoryItem struct {
	value []byte
	exp   time.Time
}

// NewInMemory creates an InMemory cache.
func NewInMemory() *InMemory {
	return &InMemory{items: make(map[string]inmemoryItem)}
}

func (m *InMemory) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	it, ok := m.items[key]
	m.mu.RUnlock()

	if !ok {
		return nil, nil
	}
	if !it.exp.IsZero() && time.Now().After(it.exp) {
		// Evict the expired entry under a write lock.
		m.mu.Lock()
		// Re-check; another goroutine may have already removed it.
		if cur, still := m.items[key]; still && !cur.exp.IsZero() && time.Now().After(cur.exp) {
			delete(m.items, key)
		}
		m.mu.Unlock()
		return nil, nil
	}
	return it.value, nil
}

func (m *InMemory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	it := inmemoryItem{value: value}
	if ttl > 0 {
		it.exp = time.Now().Add(ttl)
	}
	m.mu.Lock()
	m.items[key] = it
	m.mu.Unlock()
	return nil
}

func (m *InMemory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.items, key)
	m.mu.Unlock()
	return nil
}

// Flush removes all entries atomically and returns the count removed.
// It implements Flushable.
func (m *InMemory) Flush(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.items)
	m.items = make(map[string]inmemoryItem)
	return n, nil
}

// Len returns the number of entries currently held (including expired-but-not-yet-evicted).
// It implements Flushable.
func (m *InMemory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}
