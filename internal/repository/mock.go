package repository

import (
	"context"
	"sort"
	"time"
)

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

// All returns every row in the mock repo.
func (m *MockSubscriptionRepo) All() []*SubscriptionRow {
	out := make([]*SubscriptionRow, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, r)
	}
	return out
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

// UpdateStatus updates the subscription status for the tenant-scoped record.
func (m *MockSubscriptionRepo) UpdateStatus(_ context.Context, id string, tenantID string, status string) error {
	row, ok := m.records[id]
	if !ok {
		return ErrNotFound
	}
	if row.TenantID != tenantID {
		return ErrNotFound
	}
	row.Status = status
	return nil
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
		if q.StartAfter != "" {
			after, err := time.Parse(time.RFC3339, q.StartAfter)
			if err == nil {
				start, err := time.Parse(time.RFC3339, r.PeriodStart)
				if err != nil || !start.After(after) {
					continue
				}
			}
		}
		if q.EndBefore != "" {
			before, err := time.Parse(time.RFC3339, q.EndBefore)
			if err == nil {
				end, err := time.Parse(time.RFC3339, r.PeriodEnd)
				if err != nil || !end.Before(before) {
					continue
				}
			}
		}
		filtered = append(filtered, r)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].PeriodStart < filtered[j].PeriodStart
	})

	total := len(filtered)

	if q.Limit > 0 && q.Limit < len(filtered) {
		filtered = filtered[:q.Limit]
	}

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
	if end > len(filtered) { 
		end = len(filtered)
	}

	return filtered[start:end], total, nil
}
