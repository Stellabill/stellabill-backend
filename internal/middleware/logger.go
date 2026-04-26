package middleware

import (
	"time"

	"stellabill-backend/internal/correlation"
	"stellabill-backend/internal/logger"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {

		start := time.Now()

		requestID := correlation.NewID()
		c.Set("request_id", requestID)

		ctx := correlation.WithRequestID(c.Request.Context(), requestID)
		c.Request = c.Request.WithContext(ctx)

		if span := otel.Tracer("middleware").Start(ctx, "RequestLogger"); span != nil {
			span.SetAttributes(attribute.String("request_id", requestID))
			defer span.End()
		}

		c.Writer.Header().Set("X-Request-ID", requestID)

		c.Next()

		latency := time.Since(start)

		// Build fields with redaction applied
		fields := map[string]interface{}{
			"level":      "info",
			"request_id": requestID,
			"method":     c.Request.Method,
			"path":       security.MaskPII(c.FullPath()),
			"status":     c.Writer.Status(),
			"latency_ms": latency.Milliseconds(),
			"client_ip":  c.ClientIP(),
		}
		// Use the logger with structured fields (the Logrus hook will redact)
		logger.Log.WithFields(fields).Info("request completed")
	}
}
}
