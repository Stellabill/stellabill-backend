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

// MockStatementRepo is an in-memory StatementRepository for testing.
type MockStatementRepo struct {
	records map[string]*StatementRow
}

// NewMockStatementRepo creates a MockStatementRepo pre-populated with the given rows.
func NewMockStatementRepo(rows ...*StatementRow) *MockStatementRepo {
	m := &MockStatementRepo{records: make(map[string]*StatementRow)}
	for _, r := range rows {
		m.records[r.ID] = r
	}
	return m
}

// FindByID returns the StatementRow with the given ID, or ErrNotFound.
func (m *MockStatementRepo) FindByID(_ context.Context, id string) (*StatementRow, error) {
	row, ok := m.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	return row, nil
}

// ListByCustomerID returns statement rows for the customer matching the query.
func (m *MockStatementRepo) ListByCustomerID(_ context.Context, customerID string, q StatementQuery) ([]*StatementRow, int, error) {
	filtered := make([]*StatementRow, 0)
	for _, r := range m.records {
		if r.CustomerID != customerID {
			continue
		}
		if q.Kind != "" && r.Kind != q.Kind {
			continue
		}
		if q.Status != "" && r.Status != q.Status {
			continue
		}
		if q.SubscriptionID != "" && r.SubscriptionID != q.SubscriptionID {
			continue
		}
		filtered = append(filtered, r)
	}

	total := len(filtered)

	// In the real DB we'd have an ORDER BY. Let's do a simple stable sort for testing if needed.
	// For now, let's just paginate what we have.

	page := q.Page
	if page <= 0 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}

	start := (page - 1) * pageSize
	if start >= total {
		return []*StatementRow{}, total, nil
	}

	end := start + pageSize
	if end > total {
		end = total
	}

	return filtered[start:end], total, nil
}

