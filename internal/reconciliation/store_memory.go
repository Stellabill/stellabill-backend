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

// DeleteReportsByJobID removes all reports associated with a job ID.
func (m *MemoryStore) DeleteReportsByJobID(jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	filtered := make([]Report, 0)
	for _, report := range m.reports {
		if report.JobID != jobID {
			filtered = append(filtered, report)
		}
	}
	m.reports = filtered
	return nil
}

// GetReportsByJobID returns all reports associated with a job ID.
func (m *MemoryStore) GetReportsByJobID(jobID string) ([]Report, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	var reports []Report
	for _, report := range m.reports {
		if report.JobID == jobID {
			reports = append(reports, report)
		}
	}
	return reports, nil
}
