package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// DeprecationHeaders adds Deprecation, Sunset, and Link headers indicating the
// /api/v1 successor route for legacy /api endpoints.
func DeprecationHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Deprecation", "true")
		c.Header("Sunset", time.Now().Add(180*24*time.Hour).Format(time.RFC1123))

		path := c.Request.URL.Path
		const prefix = "/api"
		if strings.HasPrefix(path, prefix) {
			successor := prefix + "/v1" + path[len(prefix):]
			c.Header("Link", `<`+successor+`>; rel="successor-version"`)
		}

		c.Next()
	}
}

// TailSamplingSignals annotates the server span with completed request data
// used by the tracing tail decision. It must be registered after otelgin.
func TailSamplingSignals() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		span := trace.SpanFromContext(c.Request.Context())
		if !span.IsRecording() {
			return
		}
		status := c.Writer.Status()
		span.SetAttributes(
			attribute.Int("http.response.status_code", status),
			attribute.Int64("http.server.request.duration_ms", time.Since(start).Milliseconds()),
		)
		if status >= 500 {
			span.SetStatus(codes.Error, "server error")
			span.SetAttributes(attribute.Bool("error", true))
		}
	}
}

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
