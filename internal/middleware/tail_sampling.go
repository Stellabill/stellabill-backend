package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TailSamplingSignals is a Gin middleware that annotates the active server span
// with HTTP response status and error signals required by the tail-sampling
// processor to make keep/drop decisions.
//
// It must be registered after the OTel instrumentation middleware (e.g.
// otelgin.Middleware) so that an active span is already present in the context.
//
// Behaviour:
//   - Records http.response.status_code as an integer attribute.
//   - Sets span status to codes.Error and adds error=true when the HTTP
//     response status is 5xx, enabling both the OTel status-code check and the
//     error-attribute check in tailSpanProcessor.shouldKeep.
//   - 4xx and below are annotated with the status code only; the span status
//     remains unset (OTel semantic convention: 4xx is a client error, not a
//     server fault unless the handler explicitly marks it).
func TailSamplingSignals() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Let the handler chain execute first so the final status is available.
		c.Next()

		status := c.Writer.Status()
		span := trace.SpanFromContext(c.Request.Context())
		if !span.IsRecording() {
			return
		}

		span.SetAttributes(attribute.Int("http.response.status_code", status))

		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(status))
			span.SetAttributes(attribute.Bool("error", true))
		}
	}
}
