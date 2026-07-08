package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
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
