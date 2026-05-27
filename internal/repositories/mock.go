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

func (m *MockSubscriptionRepository) GetByID(id string) (*Subscription, error) {
	s, ok := m.Subscriptions[id]
	if !ok {
		return nil, fmt.Errorf("subscription not found")
	}
	return s, nil
}

func (m *MockSubscriptionRepository) GetByCustomerID(customerID string, limit, offset int) ([]*Subscription, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) GetByMerchantID(merchantID string, limit, offset int) ([]*Subscription, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) GetByPlanID(planID string, limit, offset int) ([]*Subscription, error) {
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

func (m *MockSubscriptionRepository) GetActiveSubscriptionsByMerchantID(merchantID string) ([]*Subscription, error) {
	return nil, nil
}

func (m *MockSubscriptionRepository) GetSubscriptionsDueForBilling(limit int) ([]*Subscription, error) {
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

func (m *MockPlanRepository) GetByID(id string) (*Plan, error) {
	p, ok := m.Plans[id]
	if !ok {
		return nil, fmt.Errorf("plan not found")
	}
	return p, nil
}

func (m *MockPlanRepository) GetByMerchantID(merchantID string, limit, offset int) ([]*Plan, error) {
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

func (m *MockPlanRepository) GetActivePlansByMerchantID(merchantID string) ([]*Plan, error) {
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
