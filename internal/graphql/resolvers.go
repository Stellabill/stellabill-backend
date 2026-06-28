package graphql

import (
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"

	"github.com/graphql-go/graphql"
)

// Services bundles the dependencies needed by the GraphQL resolvers.
type Services struct {
	SubSvc   service.SubscriptionService
	StmtSvc  service.StatementService
	PlanRepo repository.PlanRepository
}

// buildQueryType constructs the root Query type with all resolvers bound to svc.
func buildQueryType(svc Services) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"plans": {
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(planType))),
				Description: "List all billing plans.",
				Resolve:     resolvePlans(svc),
			},
			"subscription": {
				Type:        subscriptionType,
				Description: "Fetch a single subscription by ID (tenant-scoped).",
				Args: graphql.FieldConfigArgument{
					"id": {Type: graphql.NewNonNull(graphql.String)},
				},
				Resolve: resolveSubscription(svc),
			},
			"statements": {
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(statementType))),
				Description: "List statements for a customer (tenant-scoped).",
				Args: graphql.FieldConfigArgument{
					"customer_id": {Type: graphql.NewNonNull(graphql.String)},
				},
				Resolve: resolveStatements(svc),
			},
		},
	})
}

// resolvePlans returns all plans from the plan repository.
func resolvePlans(svc Services) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		rows, err := svc.PlanRepo.List(p.Context)
		if err != nil {
			return nil, err
		}
		result := make([]map[string]interface{}, 0, len(rows))
		for _, r := range rows {
			result = append(result, map[string]interface{}{
				"id":          r.ID,
				"name":        r.Name,
				"amount":      r.Amount,
				"currency":    r.Currency,
				"interval":    r.Interval,
				"description": r.Description,
			})
		}
		return result, nil
	}
}

// resolveSubscription fetches a tenant-scoped subscription by ID.
func resolveSubscription(svc Services) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		id, _ := p.Args["id"].(string)
		tenantID := tenantIDFromCtx(p.Context)
		callerID := callerIDFromCtx(p.Context)

		detail, _, err := svc.SubSvc.GetDetail(p.Context, tenantID, callerID, id)
		if err != nil {
			return nil, err
		}

		out := map[string]interface{}{
			"id":       detail.ID,
			"plan_id":  detail.PlanID,
			"status":   detail.Status,
			"interval": detail.Interval,
			"billing_summary": map[string]interface{}{
				"amount_cents":      int(detail.BillingSummary.AmountCents),
				"currency":          detail.BillingSummary.Currency,
				"next_billing_date": nilIfEmpty(detail.BillingSummary.NextBillingDate),
			},
		}
		if detail.Plan != nil {
			out["plan"] = map[string]interface{}{
				"id":          detail.Plan.PlanID,
				"name":        detail.Plan.Name,
				"amount":      detail.Plan.Amount,
				"currency":    detail.Plan.Currency,
				"interval":    detail.Plan.Interval,
				"description": detail.Plan.Description,
			}
		}
		return out, nil
	}
}

// resolveStatements lists statements for a customer_id, scoped by tenant.
func resolveStatements(svc Services) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		customerID, _ := p.Args["customer_id"].(string)
		callerID := callerIDFromCtx(p.Context)
		roles := rolesFromCtx(p.Context)

		stmts, _, _, err := svc.StmtSvc.ListByCustomer(
			p.Context,
			callerID,
			roles,
			customerID,
			repository.StatementQuery{Limit: 100},
		)
		if err != nil {
			return nil, err
		}

		result := make([]map[string]interface{}, 0, len(stmts.Statements))
		for _, s := range stmts.Statements {
			result = append(result, map[string]interface{}{
				"id":              s.ID,
				"subscription_id": s.SubscriptionID,
				"period_start":    s.PeriodStart,
				"period_end":      s.PeriodEnd,
				"issued_at":       s.IssuedAt,
				"total_amount":    s.TotalAmount,
				"currency":        s.Currency,
				"kind":            s.Kind,
				"status":          s.Status,
			})
		}
		return result, nil
	}
}

func nilIfEmpty(s *string) interface{} {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}
