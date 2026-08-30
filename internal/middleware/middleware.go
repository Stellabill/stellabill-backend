package middleware

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ContextKey ensures type safety for context extraction.
type ContextKey string

const (
	TenantIDKey   ContextKey = "tenant_id"
	CustomerIDKey ContextKey = "customer_id"
)

// LegacyAPISunsetEnv is the environment variable used to configure the sunset
// date for legacy /api endpoints. The value may be an HTTP-date or an RFC 3339
// timestamp (optionally JSON-quoted). Invalid values cause the Sunset header to
// be omitted while Deprecation and Link are still emitted.
const LegacyAPISunsetEnv = "LEGACY_API_SUNSET"

// defaultLegacySunset is used when LegacyAPISunsetEnv is unset.
func defaultLegacySunset(t time.Time) time.Time {
	return t.Add(180 * 24 * time.Hour)
}

// parseLegacySunset parses LegacyAPISunsetEnv into a time.Time. It returns the
// zero time and false when the value cannot be parsed.
func parseLegacySunset() (time.Time, bool) {
	raw, ok := os.LookupEnv(LegacyAPISunsetEnv)
	if !ok || strings.TrimSpace(raw) == "" {
		return time.Now(), false
	}
	raw = strings.Trim(raw, `"`)

	if t, err := http.ParseTime(raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	for _, layout := range []string{time.RFC1123, time.RFC1123Z, time.RFC822, time.RFC822Z} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// isLegacyAPIPath reports whether the request path is a legacy /api endpoint
// that should advertise its v1 successor.
func isLegacyAPIPath(path string) bool {
	return strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/api/v1")
}

// applyDeprecationHeaders writes the RFC 8594 deprecation headers onto the
// response. Headers are written before the handler runs so they survive 4xx
// responses and panics recovered by outer middleware.
func applyDeprecationHeaders(c *gin.Context) {
	path := c.Request.URL.Path
	if !isLegacyAPIPath(path) {
		return
	}

	c.Header("Deprecation", "true")

	sunset, ok := parseLegacySunset()
	if ok {
		c.Header("Sunset", sunset.UTC().Format(http.TimeFormat))
	} else if _, envSet := os.LookupEnv(LegacyAPISunsetEnv); !envSet {
		c.Header("Sunset", defaultLegacySunset(time.Now()).UTC().Format(http.TimeFormat))
	}

	successor := "/api/v1" + strings.TrimPrefix(path, "/api")
	c.Header("Link", `<`+successor+`>; rel="successor-version"`)
}

// DeprecatedHandler emits RFC 8594 deprecation headers (Deprecation, Sunset,
// Link) on legacy /api endpoints advertising their /api/v1 successor.
func DeprecatedHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		applyDeprecationHeaders(c)
		c.Next()
	}
}

// DeprecationHeaders emits RFC 8594 deprecation headers on legacy /api
// endpoints. It is kept as a companion to DeprecatedHandler for callers that
// register it as a shared middleware before groups.
func DeprecationHeaders() gin.HandlerFunc {
	return DeprecatedHandler()
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
