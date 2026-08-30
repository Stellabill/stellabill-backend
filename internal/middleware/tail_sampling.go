package middleware

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TailSamplingSignals annotates the current server span with an explicit
// error signal whenever the response indicates a server failure (>= 500), so
// tail-based samplers retain failing spans. Successful responses are marked
// as non-error so sampled traces are not biased toward healthy traffic.
//
// The middleware is a no-op when the request has no recording span (e.g. when
// tracing is disabled).
func TailSamplingSignals() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		span := trace.SpanFromContext(c.Request.Context())
		if !span.IsRecording() {
			return
		}

		code := c.Writer.Status()
		if code >= http.StatusInternalServerError {
			span.SetAttributes(attribute.Bool("error", true))
			span.SetStatus(codes.Error, "HTTP "+strconv.Itoa(code))
			return
		}
		span.SetAttributes(attribute.Bool("error", false))
	}
}
