package cache

import (
	"context"
	"time"

	"stellarbill-backend/internal/timeutil"
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
	it, ok := m.items[key]
	if !ok {
		return nil, nil
	}
	if !it.exp.IsZero() && timeutil.NowUTC().After(it.exp) {
		delete(m.items, key)
		return nil, nil
	}
	return it.value, nil
}

func (m *InMemory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	it := inmemoryItem{value: value}
	if ttl > 0 {
		it.exp = timeutil.NowUTC().Add(ttl)
	}
	m.items[key] = it
	return nil
}

func (m *InMemory) Delete(_ context.Context, key string) error {
	delete(m.items, key)
	return nil
}
