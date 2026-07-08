package saga

import (
	"context"
	"fmt"
	"time"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func CancelSubscriptionFlow(
	subSvc service.SubscriptionService,
	stmtRepo repository.StatementRepository,
	sagaID string,
	tenantID string,
	actorID string,
	subscriptionID string,
	customerID string,
	refundAmount string,
	refundCurrency string,
) *Saga {
	refundStatementID := fmt.Sprintf("stmt-%s-refund", sagaID)

	return &Saga{
		ID:   sagaID,
		Name: "cancel_subscription_with_refund",
		Context: NewSagaContext(map[string]any{
			"tenant_id":           tenantID,
			"actor_id":            actorID,
			"subscription_id":     subscriptionID,
			"customer_id":         customerID,
			"refund_amount":       refundAmount,
			"refund_currency":     refundCurrency,
			"refund_statement_id": refundStatementID,
		}),
		Steps: []Step{
			{
				Key: "cancel_subscription",
				Execute: func(ctx context.Context, sc SagaContext) error {
					result, err := subSvc.ChangeStatus(ctx, tenantID, actorID, subscriptionID, "cancelled")
					if err != nil {
						return fmt.Errorf("cancel subscription: %w", err)
					}
					sc.Set("previous_status", result.PreviousStatus)
					return nil
				},
				Compensate: func(ctx context.Context, sc SagaContext) error {
					prev, ok := sc.Get("previous_status")
					if !ok {
						return fmt.Errorf("previous status not found in saga context")
					}
					_, err := subSvc.ChangeStatus(ctx, tenantID, actorID, subscriptionID, prev.(string))
					if err != nil {
						return fmt.Errorf("restore subscription status to %s: %w", prev, err)
					}
					return nil
				},
			},
			{
				Key: "create_refund_statement",
				Execute: func(ctx context.Context, sc SagaContext) error {
					now := time.Now().UTC().Format(time.RFC3339)
					stmt := &repository.StatementRow{
						ID:             refundStatementID,
						SubscriptionID: subscriptionID,
						CustomerID:     customerID,
						PeriodStart:    now,
						PeriodEnd:      now,
						IssuedAt:       now,
						TotalAmount:    refundAmount,
						Currency:       refundCurrency,
						Kind:           "refund",
						Status:         "issued",
					}
					if err := stmtRepo.Create(ctx, stmt); err != nil {
						return fmt.Errorf("create refund statement: %w", err)
					}
					sc.Set("refund_created", true)
					return nil
				},
				Compensate: func(ctx context.Context, sc SagaContext) error {
					created, _ := sc.Get("refund_created")
					if created == nil || !created.(bool) {
						return nil
					}
					existing, err := stmtRepo.FindByID(ctx, refundStatementID)
					if err != nil {
						if err == repository.ErrNotFound {
							return nil
						}
						return fmt.Errorf("find refund statement for void: %w", err)
					}
					existing.Status = "voided"
					if err := stmtRepo.UpdateArchivedData(ctx, refundStatementID, existing); err != nil {
						return fmt.Errorf("void refund statement: %w", err)
					}
					return nil
				},
			},
		},
	}
}
