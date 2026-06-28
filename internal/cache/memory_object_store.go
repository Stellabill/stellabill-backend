package cache

import (
	"context"
	"sync"
)

// MemoryObjectStore is an in-memory implementation of ObjectStore for testing and development.
// It is NOT thread-safe by default; use NewMemoryObjectStore for a thread-safe version.
type MemoryObjectStore struct {
	mu      sync.RWMutex
	objects map[string][]byte
}

// NewMemoryObjectStore creates a new thread-safe in-memory object store.
func NewMemoryObjectStore() *MemoryObjectStore {
	return &MemoryObjectStore{
		objects: make(map[string][]byte),
	}
}

// Put stores data at the given key.
func (m *MemoryObjectStore) Put(ctx context.Context, key string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Copy the data to avoid mutations from caller
	buf := make([]byte, len(data))
	copy(buf, data)
	m.objects[key] = buf

	return key, nil
}

// Get retrieves data from the given key.
func (m *MemoryObjectStore) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, ok := m.objects[key]
	if !ok {
		return nil, ErrNotFound{}
	}

	// Return a copy to prevent caller mutations
	result := make([]byte, len(data))
	copy(result, data)
	return result, nil
}

// Delete removes data at the given key.
// Returns ErrNotFound if the key doesn't exist (idempotent).
func (m *MemoryObjectStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.objects[key]; !ok {
		return ErrNotFound{}
	}

	delete(m.objects, key)
	return nil
}

// All returns all stored objects (for testing/inspection).
func (m *MemoryObjectStore) All() map[string][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]byte)
	for k, v := range m.objects {
		result[k] = v
	}
	return result
}

// Clear removes all objects (for testing).
func (m *MemoryObjectStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects = make(map[string][]byte)
}
