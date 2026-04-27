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
	listErr error
	findErr error
}

// NewMockStatementRepo creates a MockStatementRepo pre-populated with the given rows.
func NewMockStatementRepo(rows ...*StatementRow) *MockStatementRepo {
	m := &MockStatementRepo{records: make(map[string]*StatementRow)}
	for _, r := range rows {
		m.records[r.ID] = r
	}
	return m
}

func (m *MockStatementRepo) SetListError(err error) {
	m.listErr = err
}

func (m *MockStatementRepo) SetFindError(err error) {
	m.findErr = err
}

// FindByID returns the StatementRow with the given ID, or ErrNotFound.
func (m *MockStatementRepo) FindByID(_ context.Context, id string) (*StatementRow, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	row, ok := m.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	return row, nil
}

// ListByCustomerID returns statement rows for the customer matching the query.
func (m *MockStatementRepo) ListByCustomerID(_ context.Context, customerID string, q StatementQuery) ([]*StatementRow, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	out := make([]*StatementRow, 0)
	for _, r := range m.records {
		if r.CustomerID != customerID {
			continue
		}
		if q.SubscriptionID != "" && r.SubscriptionID != q.SubscriptionID {
			continue
		}
		if q.Kind != "" && r.Kind != q.Kind {
			continue
		}
		if q.Status != "" && r.Status != q.Status {
			continue
		}
		// Basic simulated filtering for period checks
		if q.StartAfter != "" && r.PeriodStart < q.StartAfter {
			continue
		}
		if q.EndBefore != "" && r.PeriodEnd > q.EndBefore {
			continue
		}

		out = append(out, r)
	}
	totalCount := len(out)
	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, totalCount, nil
}
