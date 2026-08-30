package cache

import (
	"context"
	"stellarbill-backend/internal/servertiming"
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

// InMemory is a simple in-memory cache used for tests and default runs.
type InMemory struct {
	items map[string]inmemoryItem
	mu    sync.RWMutex
}

type inmemoryItem struct {
	value []byte
	exp   time.Time
}

// NewInMemory creates an InMemory cache.
func NewInMemory() *InMemory {
	return &InMemory{items: make(map[string]inmemoryItem)}
}

func (m *InMemory) Get(ctx context.Context, key string) ([]byte, error) {
	start := time.Now()
	defer func() {
		if rec := servertiming.FromContext(ctx); rec != nil {
			rec.RecordCache(time.Since(start))
		}
	}()
	m.mu.RLock()
	it, ok := m.items[key]
	m.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	if !it.exp.IsZero() && time.Now().After(it.exp) {
		m.mu.Lock()
		delete(m.items, key)
		m.mu.Unlock()
		return nil, nil
	}
	return it.value, nil
}

func (m *InMemory) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	start := time.Now()
	defer func() {
		if rec := servertiming.FromContext(ctx); rec != nil {
			rec.RecordCache(time.Since(start))
		}
	}()
	m.mu.Lock()
	it := inmemoryItem{value: value}
	if ttl > 0 {
		it.exp = time.Now().Add(ttl)
	}
	m.items[key] = it
	m.mu.Unlock()
	return nil
}

func (m *InMemory) Delete(ctx context.Context, key string) error {
	start := time.Now()
	defer func() {
		if rec := servertiming.FromContext(ctx); rec != nil {
			rec.RecordCache(time.Since(start))
		}
	}()
	m.mu.Lock()
	delete(m.items, key)
	m.mu.Unlock()
	return nil
}

// Len returns the number of items currently stored.
func (m *InMemory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}

// Flush removes all items and returns the number removed. It is idempotent:
// flushing an already-empty cache returns 0 and no error.
func (m *InMemory) Flush(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.items)
	for k := range m.items {
		delete(m.items, k)
	}
	return n, nil
}

// flusher is implemented by caches that support bulk eviction.
type flusher interface {
	Flush(ctx context.Context) (int, error)
}

// Flush evicts all entries from c when the underlying cache supports bulk
// flushing. It returns the number of keys removed.
func Flush(ctx context.Context, c Cache) (int, error) {
	if f, ok := c.(flusher); ok {
		return f.Flush(ctx)
	}
	return 0, nil
}

// GuardedCache wraps a Cache with per-key stampede protection.
// Only one goroutine per key executes the loader; others wait and share the result.
type GuardedCache struct {
	cache Cache
	locks sync.Map // map[string]*sync.Mutex
}

// NewGuardedCache wraps the provided Cache with stampede protection.
func NewGuardedCache(c Cache) *GuardedCache {
	return &GuardedCache{cache: c}
}

// GetOrLoad checks cache first. On miss, it locks per-key, re-checks cache,
// then calls loader() if still missing. The loaded value is stored with ttl.
func (g *GuardedCache) GetOrLoad(ctx context.Context, key string, ttl time.Duration, loader func() ([]byte, error)) ([]byte, error) {
	// Fast path: cache hit without locking
	if val, err := g.cache.Get(ctx, key); err == nil && val != nil {
		return val, nil
	}

	// Slow path: acquire per-key lock
	muInt, _ := g.locks.LoadOrStore(key, &sync.Mutex{})
	mu := muInt.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// Double-check: another goroutine may have loaded while we waited
	if val, err := g.cache.Get(ctx, key); err == nil && val != nil {
		return val, nil
	}

	// Execute loader (DB query) — only this goroutine
	data, err := loader()
	if err != nil {
		return nil, err
	}

	// Store in cache
	if err := g.cache.Set(ctx, key, data, ttl); err != nil {
		// Non-fatal: we can still return the loaded data
	}
	return data, nil
}

// Delete delegates to the underlying cache.
func (g *GuardedCache) Delete(ctx context.Context, key string) error {
	return g.cache.Delete(ctx, key)
}
