package repositories

import (
	"context"
	"fmt"
	"stellarbill-backend/internal/db"
)

type MockSubscriptionRepository struct {
	Subscriptions map[string]*Subscription
}

func NewMockSubscriptionRepository() *MockSubscriptionRepository {
	return &MockSubscriptionRepository{
		Subscriptions: make(map[string]*Subscription),
	}
}

func (m *MockSubscriptionRepository) Create(s *Subscription) error {
	m.Subscriptions[s.ID] = s
	return nil
}

func (m *MockSubscriptionRepository) GetByID(ctx context.Context, id string) (*Subscription, error) {
	s, ok := m.Subscriptions[id]
	if !ok {
		return nil, fmt.Errorf("subscription not found")
	}
	return s, nil
}

func (m *MockSubscriptionRepository) GetByCustomerID(ctx context.Context, customerID string, limit, offset int) ([]*Subscription, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) GetByMerchantID(ctx context.Context, merchantID string, limit, offset int) ([]*Subscription, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) GetByPlanID(ctx context.Context, planID string, limit, offset int) ([]*Subscription, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) Update(s *Subscription) error {
	m.Subscriptions[s.ID] = s
	return nil
}

func (m *MockSubscriptionRepository) UpdateStatus(id string, status string) error {
	if s, ok := m.Subscriptions[id]; ok {
		s.Status = status
		return nil
	}
	return fmt.Errorf("subscription not found")
}

func (m *MockSubscriptionRepository) Cancel(id string, cancelAtPeriodEnd bool) error {
	return nil
}

func (m *MockSubscriptionRepository) GetActiveSubscriptionsByMerchantID(ctx context.Context, merchantID string) ([]*Subscription, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) GetSubscriptionsDueForBilling(ctx context.Context, limit int) ([]*Subscription, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) WithTx(tx db.DBTX) SubscriptionRepository {
	return m
}

type MockPlanRepository struct {
	Plans map[string]*Plan
}

func NewMockPlanRepository() *MockPlanRepository {
	return &MockPlanRepository{
		Plans: make(map[string]*Plan),
	}
}

func (m *MockPlanRepository) Create(p *Plan) error {
	m.Plans[p.ID] = p
	return nil
}

func (m *MockPlanRepository) GetByID(ctx context.Context, id string) (*Plan, error) {
	p, ok := m.Plans[id]
	if !ok {
		return nil, fmt.Errorf("plan not found")
	}
	return p, nil
}

func (m *MockPlanRepository) GetByMerchantID(ctx context.Context, merchantID string, limit, offset int) ([]*Plan, error) {
	return nil, nil
}

func (m *MockPlanRepository) Update(p *Plan) error {
	m.Plans[p.ID] = p
	return nil
}

func (m *MockPlanRepository) Delete(id string) error {
	delete(m.Plans, id)
	return nil
}

func (m *MockPlanRepository) GetActivePlansByMerchantID(ctx context.Context, merchantID string) ([]*Plan, error) {
	return nil, nil
}

func (m *MockPlanRepository) List(ctx context.Context) ([]*Plan, error) {
	var list []*Plan
	for _, p := range m.Plans {
		list = append(list, p)
	}
	return list, nil
}

func (m *MockPlanRepository) WithTx(tx db.DBTX) PlanRepository {
	return m
}
