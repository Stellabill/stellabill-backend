package repository

import "context"

// MockSubscriptionRepo is an in-memory SubscriptionRepository for testing.
type MockSubscriptionRepo struct {
	records map[string]*SubscriptionRow
}

// NewMockSubscriptionRepo creates a MockSubscriptionRepo pre-populated with the given rows.
func NewMockSubscriptionRepo(rows ...*SubscriptionRow) *MockSubscriptionRepo {
	m := &MockSubscriptionRepo{records: make(map[string]*SubscriptionRow)}
	for _, r := range rows {
		m.records[r.ID] = r
	}
	return m
}

// FindByID returns the SubscriptionRow with the given ID, or ErrNotFound.
func (m *MockSubscriptionRepo) FindByID(_ context.Context, id string) (*SubscriptionRow, error) {
	row, ok := m.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	return row, nil
}

func (m *MockSubscriptionRepo) FindByIDAndTenant(_ context.Context, id string, tenantID string) (*SubscriptionRow, error) {
	row, ok := m.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	if row.TenantID != tenantID {
		return nil, ErrNotFound
	}
	return row, nil
}

// MockPlanRepo is an in-memory PlanRepository for testing.
type MockPlanRepo struct {
	records map[string]*PlanRow
}

// NewMockPlanRepo creates a MockPlanRepo pre-populated with the given rows.
func NewMockPlanRepo(rows ...*PlanRow) *MockPlanRepo {
	m := &MockPlanRepo{records: make(map[string]*PlanRow)}
	for _, r := range rows {
		m.records[r.ID] = r
	}
	return m
}

// FindByID returns the PlanRow with the given ID, or ErrNotFound.
func (m *MockPlanRepo) FindByID(_ context.Context, id string) (*PlanRow, error) {
	row, ok := m.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	return row, nil
}

// List returns all PlanRows stored in the mock repository.
func (m *MockPlanRepo) List(_ context.Context) ([]*PlanRow, error) {
	out := make([]*PlanRow, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, r)
	}
	return out, nil
}
