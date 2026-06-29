package middleware

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/baggage"
)

// ContextKey ensures type safety for context extraction.
type ContextKey string

const (
	TenantIDKey   ContextKey = "tenant_id"
	CustomerIDKey ContextKey = "customer_id"
)

// BaggageMiddleware extracts tenant and customer IDs and populates the OpenTelemetry Baggage context.
func BaggageMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		tenantID, _ := ctx.Value(TenantIDKey).(string)
		customerID, _ := ctx.Value(CustomerIDKey).(string)

		var members []baggage.Member

		if tenantID != "" {
			if m, err := baggage.NewMember("tenant_id", tenantID); err == nil {
				members = append(members, m)
			}
		}
		if customerID != "" {
			if m, err := baggage.NewMember("customer_id", customerID); err == nil {
				members = append(members, m)
			}
		}

		if len(members) > 0 {
			if bag, err := baggage.New(members...); err == nil {
				ctx = baggage.ContextWithBaggage(ctx, bag)
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}