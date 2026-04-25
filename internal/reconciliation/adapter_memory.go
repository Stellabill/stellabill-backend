package reconciliation

import "context"

// MemoryAdapter is a simple in-memory Adapter useful for tests and local runs.
type MemoryAdapter struct {
	snapshots map[string]Snapshot
}

// NewMemoryAdapter creates a MemoryAdapter preloaded with given snapshots.
func NewMemoryAdapter(snaps ...Snapshot) *MemoryAdapter {
	m := &MemoryAdapter{snapshots: make(map[string]Snapshot)}
	for _, s := range snaps {
		m.snapshots[s.SubscriptionID] = s
	}
	return m
}

// FetchSnapshots returns all snapshots stored in memory.
func (m *MemoryAdapter) FetchSnapshots(ctx context.Context) ([]Snapshot, error) {
	out := make([]Snapshot, 0, len(m.snapshots))
	for _, s := range m.snapshots {
		out = append(out, s)
	}
	return out, nil
}
