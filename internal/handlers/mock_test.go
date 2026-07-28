package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/mock"
	"stellarbill-backend/internal/service"
)

type MockPlanService struct {
	mock.Mock
}

func (m *MockPlanService) ListPlans(c *gin.Context) ([]Plan, error) {
	args := m.Called(c)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]Plan), args.Error(1)
}

type MockSubscriptionService struct {
	mock.Mock
}

func (m *MockSubscriptionService) ListSubscriptions(c *gin.Context) ([]Subscription, error) {
	args := m.Called(c)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]Subscription), args.Error(1)
}

func (m *MockSubscriptionService) GetSubscription(c *gin.Context, id string) (*Subscription, error) {
	args := m.Called(c, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Subscription), args.Error(1)
}

func (m *MockSubscriptionService) BatchChangeStatus(c *gin.Context, tenantID string, actorID string, operations []service.BatchSubscriptionOperation) ([]service.BatchSubscriptionResult, error) {
	args := m.Called(c, tenantID, actorID, operations)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]service.BatchSubscriptionResult), args.Error(1)
}
