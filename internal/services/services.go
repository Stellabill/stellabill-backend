package services

import (
	"stellabill-backend/internal/handlers"

	"github.com/gin-gonic/gin"
)

type planService struct{}

func NewPlanService() handlers.PlanService {
	return &planService{}
}

func (s *planService) ListPlans(c *gin.Context) ([]handlers.Plan, error) {
	return []handlers.Plan{}, nil
}

type subscriptionService struct{}

func NewSubscriptionService() handlers.SubscriptionService {
	return &subscriptionService{}
}

func (s *subscriptionService) ListSubscriptions(c *gin.Context) ([]handlers.Subscription, error) {
	return []handlers.Subscription{}, nil
}

func (s *subscriptionService) GetSubscription(c *gin.Context, id string) (*handlers.Subscription, error) {
	return &handlers.Subscription{ID: id, Status: "placeholder"}, nil
}

