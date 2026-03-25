package middleware

import (
	"time"

	"stellabill-backend/internal/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {

		start := time.Now()

		requestID := uuid.New().String()
		c.Set("request_id", requestID)

		c.Writer.Header().Set("X-Request-ID", requestID)

		c.Next()

		latency := time.Since(start)

		logger.Log.WithFields(map[string]interface{}{
			"level":      "info",
			"request_id": requestID,
			"method":     c.Request.Method,
			"path":       c.Request.URL.Path,
			"status":     c.Writer.Status(),
			"latency_ms": latency.Milliseconds(),
			"client_ip":  c.ClientIP(),
		}).Info("request completed")
	}
}