package middleware

import (
	"net/http"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
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

// ExemplarAwareMiddleware ensures that the OpenTelemetry span context is
// propagated through the request lifecycle so that downstream metrics
// middleware can attach exemplars (trace_id, span_id) to Prometheus histograms.
//
// This middleware must be placed AFTER the OTel tracing middleware
// (e.g. otelgin.Middleware) in the handler chain so that the span context
// is already populated in the request context.
//
// When the active span is sampled and recording, the metrics middleware can
// use tracing.ExemplarLabels(ctx) to extract trace identifiers for exemplars.
// When the span is not sampled or absent, exemplars are skipped, preserving
// backward compatibility with Prometheus scrapes that do not enable exemplar
// storage.
//
// This middleware does not modify the context itself — it acts as a contract
// checkpoint that verifies span propagation and can be used for observability.
func ExemplarAwareMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		sc := span.SpanContext()

		// Propagate a custom header indicating whether exemplars will be available
		// for this request. This helps downstream services understand the tracing
		// state without re-extracting the span context.
		if sc.IsValid() && sc.IsSampled() && span.IsRecording() {
			w.Header().Set("X-Exemplar-Available", "true")
		}

		next.ServeHTTP(w, r)
	})
}
