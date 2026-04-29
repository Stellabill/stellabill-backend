package service

import (
	"context"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/security"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("service/subscriptions")

// SubscriptionService defines the business logic interface for subscriptions.
type SubscriptionService interface {
	GetDetail(ctx context.Context, tenantID string, callerID string, subscriptionID string) (*SubscriptionDetail, []string, error)
}

// subscriptionService is the concrete implementation of SubscriptionService.
type subscriptionService struct {
	subRepo  repository.SubscriptionRepository
	planRepo repository.PlanRepository
}

// NewSubscriptionService constructs a SubscriptionService with the given repositories.
func NewSubscriptionService(subRepo repository.SubscriptionRepository, planRepo repository.PlanRepository) SubscriptionService {
	return &subscriptionService{subRepo: subRepo, planRepo: planRepo}
}

// GetDetail retrieves a full SubscriptionDetail for the given subscriptionID.
// It enforces ownership (callerID must match the subscription's CustomerID),
//
 // handles soft-deletes, joins plan metadata, and normalizes billing fields.
func (s *subscriptionService) GetDetail(ctx context.Context, tenantID string, callerID string, subscriptionID string) (*SubscriptionDetail, []string, error) {
	ctx, span := tracer.Start(ctx, "SubscriptionService.GetDetail",
		trace.WithAttributes(
			attribute.String("subscription.id", subscriptionID),
			attribute.String("caller.id", callerID),
		))
	defer span.End()

	var warnings []string

	// 1. Fetch subscription row scoped to tenant.
	row, err := s.subRepo.FindByIDAndTenant(ctx, subscriptionID, tenantID)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}

	// 2. Soft-delete check.
	if row.DeletedAt != nil {
		return nil, nil, ErrDeleted
	}

	// 3. Ownership check.
	if callerID != row.CustomerID {
		return nil, nil, ErrForbidden
	}

	// 4. Fetch plan metadata (non-fatal if missing).
	var planMeta *PlanMetadata
	planRow, err := s.planRepo.FindByID(ctx, row.PlanID)
	if err != nil {
		if err == repository.ErrNotFound {
			warnings = append(warnings, "plan not found")
		} else {
			return nil, nil, err
		}
	} else {
		planMeta = &PlanMetadata{
			PlanID:      planRow.ID,
			Name:        planRow.Name,
			Amount:      planRow.Amount,
			Currency:    planRow.Currency,
			Interval:    planRow.Interval,
			Description: planRow.Description,
		}
	}

	// 5. Parse amount to int64 cents.
	amountCents, parseErr := strconv.ParseInt(row.Amount, 10, 64)
	if parseErr != nil {
		security.ProductionLogger().Error("failed to parse amount",
			zap.String("amount", row.Amount),
			zap.String("subscription_id", row.ID),
			zap.Error(parseErr))
		return nil, nil, ErrBillingParse
	}

	// 6. Build BillingSummary.
	var nextBillingDate *string
	if row.NextBilling != "" {
		nb := row.NextBilling
		nextBillingDate = &nb
	}

	billing := BillingSummary{
		AmountCents:     amountCents,
		Currency:        strings.ToUpper(row.Currency),
		NextBillingDate: nextBillingDate,
	}

	// 7. Build SubscriptionDetail — CustomerID is mapped to Customer (safe to expose).
	detail := &SubscriptionDetail{
		ID:             row.ID,
		PlanID:         row.PlanID,
		Customer:       row.CustomerID,
		Status:         row.Status,
		Interval:       row.Interval,
		Plan:           planMeta,
		BillingSummary: billing,
	}

	// 8. Return detail and warnings.
	return detail, warnings, nil
}

