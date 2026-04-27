package ingestion

import (
	"context"
	"sync"
)

// MockRepository is an in-memory EventRepository for testing.
type MockRepository struct {
	mu     sync.Mutex
	events []*ContractEvent
	byKey  map[string]bool
	byID   map[string]*ContractEvent

	// InsertErr can be set to simulate persistence failures.
	InsertErr error
}

// NewMockRepository creates a ready-to-use MockRepository.
func NewMockRepository() *MockRepository {
	return &MockRepository{
		byKey: make(map[string]bool),
		byID:  make(map[string]*ContractEvent),
	}
}

func (m *MockRepository) Insert(_ context.Context, event *ContractEvent) error {
	if m.InsertErr != nil {
		return m.InsertErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	m.byKey[event.IdempotencyKey] = true
	m.byID[event.ID] = event
	return nil
}

func (m *MockRepository) ExistsByIdempotencyKey(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byKey[key], nil
}

func (m *MockRepository) FindByID(_ context.Context, id string) (*ContractEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.byID[id]; ok {
		return e, nil
	}
	return nil, nil
}

func (m *MockRepository) ListByContractID(_ context.Context, contractID string, limit, offset int) ([]*ContractEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*ContractEvent
	for _, e := range m.events {
		if e.ContractID == contractID {
			result = append(result, e)
		}
	}
	if offset >= len(result) {
		return nil, nil
	}
	result = result[offset:]
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}
	return result, nil
}

func (m *MockRepository) LatestSequenceForContract(_ context.Context, contractID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var max int64
	for _, e := range m.events {
		if e.ContractID == contractID && e.SequenceNum > max {
			max = e.SequenceNum
		}
	}
	return max, nil
}

