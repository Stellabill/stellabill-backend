package grpc

import (
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/repository"

	"github.com/gin-gonic/gin"
)

// PlanServiceWrapper wraps a PlanRepository into the handlers.PlanService interface.
type PlanServiceWrapper struct {
	planRepo repository.PlanRepository
}

// NewPlanServiceWrapper creates a new PlanServiceWrapper.
func NewPlanServiceWrapper(planRepo repository.PlanRepository) *PlanServiceWrapper {
	return &PlanServiceWrapper{planRepo: planRepo}
}

// ListPlans implements handlers.PlanService by reading from the repository.
func (w *PlanServiceWrapper) ListPlans(c *gin.Context) ([]handlers.Plan, error) {
	rows, err := w.planRepo.List(c.Request.Context())
	if err != nil {
		return nil, err
	}
	plans := make([]handlers.Plan, 0, len(rows))
	for _, row := range rows {
		plans = append(plans, handlers.Plan{
			ID:          row.ID,
			Name:        row.Name,
			Amount:      row.Amount,
			Currency:    row.Currency,
			Interval:    row.Interval,
			Description: row.Description,
		})
	}
	return plans, nil
}

// SubscriptionServiceWrapper wraps repositories into the handlers.SubscriptionService interface.
type SubscriptionServiceWrapper struct {
	subRepo repository.SubscriptionRepository
	planRepo repository.PlanRepository
}

// NewSubscriptionServiceWrapper creates a new SubscriptionServiceWrapper.
func NewSubscriptionServiceWrapper(subRepo repository.SubscriptionRepository, planRepo repository.PlanRepository) *SubscriptionServiceWrapper {
	return &SubscriptionServiceWrapper{
		subRepo:  subRepo,
		planRepo: planRepo,
	}
}

// ListSubscriptions implements handlers.SubscriptionService.
func (w *SubscriptionServiceWrapper) ListSubscriptions(c *gin.Context) ([]handlers.Subscription, error) {
	// For the mock layer, return all subscriptions (no tenant filtering in mock)
	rows, err := w.subRepo.ListByTenant(c.Request.Context(), "")
	if err != nil {
		return nil, err
	}
	subs := make([]handlers.Subscription, 0, len(rows))
	for _, row := range rows {
		subs = append(subs, toSubscription(row))
	}
	return subs, nil
}

// GetSubscription implements handlers.SubscriptionService.
func (w *SubscriptionServiceWrapper) GetSubscription(c *gin.Context, id string) (*handlers.Subscription, error) {
	row, err := w.subRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		return nil, err
	}
	sub := toSubscription(row)
	return &sub, nil
}

func toSubscription(row *repository.SubscriptionRow) handlers.Subscription {
	sub := handlers.Subscription{
		ID:       row.ID,
		PlanID:   row.PlanID,
		Customer: row.CustomerID,
		Status:   row.Status,
		Amount:   row.Amount,
		Interval: row.Interval,
	}
	if row.NextBilling != "" {
		sub.NextBilling = row.NextBilling
	}
	return sub
}
