package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

// TraceIDMiddleware injects a trace ID into the request context for observability.
// It prioritizes the OpenTelemetry trace ID if a span is present.
func TraceIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var traceID string

		// 1. Try to get trace ID from OpenTelemetry span
		spanContext := trace.SpanContextFromContext(c.Request.Context())
		if spanContext.IsValid() {
			traceID = spanContext.TraceID().String()
		}

		// 2. Fallback to X-Trace-ID header or new UUID
		if traceID == "" {
			traceID = c.GetHeader("X-Trace-ID")
			if traceID == "" {
				traceID = uuid.New().String()
			}
		}

		// Set trace ID in Gin context for potential downstream use
		c.Set("traceID", traceID)

		// Pass trace ID to response headers for client tracking
		c.Header("X-Trace-ID", traceID)

		c.Next()
	}
}
