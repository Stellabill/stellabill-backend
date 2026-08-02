package graphql

import (
	"strconv"
	"strings"

	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/timeutil"

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

// resolvePlanField fetches the plan associated with a subscription using the dataloader.
func resolvePlanField(p graphql.ResolveParams) (interface{}, error) {
		source, ok := p.Source.(map[string]interface{})
		if !ok {
			return nil, nil
		}
		planID, _ := source["plan_id"].(string)
		if planID == "" {
			return nil, nil
		}
		tenantID := tenantIDFromCtx(p.Context)

		loader := repository.LoaderFromContext(p.Context)
		if loader == nil {
			// Fallback: dataloader not found (e.g. in legacy tests), return eager loaded map
			if planMap, ok := source["plan"].(map[string]interface{}); ok {
				return planMap, nil
			}
			return nil, nil
		}

		return func() (interface{}, error) {
			plan, err := loader.LoadPlan(p.Context, tenantID, planID)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"id":          plan.ID,
				"name":        plan.Name,
				"amount":      plan.Amount,
				"currency":    plan.Currency,
				"interval":    plan.Interval,
				"description": plan.Description,
			}, nil
		}, nil
	}
}

// resolveSubscriptionField fetches the subscription associated with a statement using the dataloader.
func resolveSubscriptionField(p graphql.ResolveParams) (interface{}, error) {
		source, ok := p.Source.(map[string]interface{})
		if !ok {
			return nil, nil
		}
		subID, _ := source["subscription_id"].(string)
		if subID == "" {
			return nil, nil
		}
		tenantID := tenantIDFromCtx(p.Context)

		loader := repository.LoaderFromContext(p.Context)
		if loader == nil {
			return nil, nil
		}

		return func() (interface{}, error) {
			sub, err := loader.LoadSubscription(p.Context, tenantID, subID)
			if err != nil {
				return nil, err
			}
			
			// Build billing_summary manually since we are bypassing GetDetail
			amountCents, _ := strconv.ParseInt(sub.Amount, 10, 64)
			var nextBillingDate *string
			if sub.NextBilling != "" {
				if nb, err := timeutil.NormalizeRFC3339StringToUTC(sub.NextBilling); err == nil {
					nextBillingDate = &nb
				}
			}

			return map[string]interface{}{
				"id":       sub.ID,
				"plan_id":  sub.PlanID,
				"status":   sub.Status,
				"interval": sub.Interval,
				"billing_summary": map[string]interface{}{
					"amount_cents":      int(amountCents),
					"currency":          strings.ToUpper(sub.Currency),
					"next_billing_date": nilIfEmpty(nextBillingDate),
				},
			}, nil
		}, nil
	}
}
