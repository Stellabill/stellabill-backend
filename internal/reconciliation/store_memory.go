package reconciliation

import "sync"

// MemoryStore is a thread-safe in-memory store for reports. Useful for local/dev and tests.
type MemoryStore struct {
	mu      sync.RWMutex
	reports []Report
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{reports: make([]Report, 0)}
}

// SaveReports appends reports to the in-memory list.
func (m *MemoryStore) SaveReports(reports []Report) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reports = append(m.reports, reports...)
	return nil
}

// ListReports returns a copy of stored reports.
func (m *MemoryStore) ListReports() ([]Report, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Report, len(m.reports))
	copy(out, m.reports)
	return out, nil
}

