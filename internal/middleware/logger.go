package middleware

import (
    "time"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "stellarbill-backend/internal/logger"
    "stellarbill-backend/internal/correlation"
)

// RequestLogger enriches each request with IDs and logs request details.
func RequestLogger() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()

        // Ensure request ID exists (fallback)
        requestID := c.GetString("request_id")
        if requestID == "" {
            requestID = uuid.New().String()
            c.Set("request_id", requestID)
            c.Writer.Header().Set("X-Request-ID", requestID)
        }

        // Ensure correlation ID exists (fallback)
        correlationID := c.GetString("correlation_id")
        if correlationID == "" {
            correlationID = correlation.NewID()
            c.Set("correlation_id", correlationID)
            c.Writer.Header().Set("X-Correlation-ID", correlationID)
        }

        // Ensure trace ID exists (fallback)
        traceID := c.GetString("traceID")
        if traceID == "" {
            // TraceIDMiddleware should set this, but fallback to new UUID
            traceID = uuid.New().String()
            c.Set("traceID", traceID)
            c.Writer.Header().Set("X-Trace-ID", traceID)
        }

        c.Next()

        latency := time.Since(start)
        logger.Log.WithFields(map[string]interface{}{
            "level":          "info",
            "request_id":     requestID,
            "correlation_id": correlationID,
            "trace_id":       traceID,
            "method":         c.Request.Method,
            "path":           c.Request.URL.Path,
            "status":         c.Writer.Status(),
            "latency_ms":     latency.Milliseconds(),
            "client_ip":      c.ClientIP(),
        }).Info("request completed")
    }
}
