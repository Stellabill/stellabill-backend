// Package graphql provides a thin GraphQL gateway over the existing services.
// It exposes Plan, Subscription, and Statement types through a single endpoint,
// enforces tenant scoping from the Gin context, and applies depth/complexity limits.
package graphql

import (
	"github.com/graphql-go/graphql"
)

// planType is the GraphQL object type for a billing plan.
var planType = graphql.NewObject(graphql.ObjectConfig{
	Name: "Plan",
	Fields: graphql.Fields{
		"id":          {Type: graphql.NewNonNull(graphql.String)},
		"name":        {Type: graphql.NewNonNull(graphql.String)},
		"amount":      {Type: graphql.NewNonNull(graphql.String)},
		"currency":    {Type: graphql.NewNonNull(graphql.String)},
		"interval":    {Type: graphql.NewNonNull(graphql.String)},
		"description": {Type: graphql.String},
	},
})

// billingSummaryType is embedded inside subscriptionType.
var billingSummaryType = graphql.NewObject(graphql.ObjectConfig{
	Name: "BillingSummary",
	Fields: graphql.Fields{
		"amount_cents":      {Type: graphql.NewNonNull(graphql.Int)},
		"currency":          {Type: graphql.NewNonNull(graphql.String)},
		"next_billing_date": {Type: graphql.String},
	},
})

// subscriptionType is the GraphQL object type for a subscription.
var subscriptionType = graphql.NewObject(graphql.ObjectConfig{
	Name: "Subscription",
	Fields: graphql.Fields{
		"id":              {Type: graphql.NewNonNull(graphql.String)},
		"plan_id":         {Type: graphql.NewNonNull(graphql.String)},
		"status":          {Type: graphql.NewNonNull(graphql.String)},
		"interval":        {Type: graphql.NewNonNull(graphql.String)},
		"billing_summary": {Type: graphql.NewNonNull(billingSummaryType)},
		"plan":            {Type: planType},
	},
})

// statementType is the GraphQL object type for a billing statement.
var statementType = graphql.NewObject(graphql.ObjectConfig{
	Name: "Statement",
	Fields: graphql.Fields{
		"id":              {Type: graphql.NewNonNull(graphql.String)},
		"subscription_id": {Type: graphql.NewNonNull(graphql.String)},
		"period_start":    {Type: graphql.NewNonNull(graphql.String)},
		"period_end":      {Type: graphql.NewNonNull(graphql.String)},
		"issued_at":       {Type: graphql.NewNonNull(graphql.String)},
		"total_amount":    {Type: graphql.NewNonNull(graphql.String)},
		"currency":        {Type: graphql.NewNonNull(graphql.String)},
		"kind":            {Type: graphql.NewNonNull(graphql.String)},
		"status":          {Type: graphql.NewNonNull(graphql.String)},
	},
})
