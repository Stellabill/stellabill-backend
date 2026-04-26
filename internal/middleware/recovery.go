package middleware

import (
	"net/http"

	"stellarbill-backend/internal/logger"
	"stellarbill-backend/internal/security"

	"github.com/gin-gonic/gin"
)

func RecoveryLogger() gin.HandlerFunc {
	return func(c *gin.Context) {

		defer func() {
			if err := recover(); err != nil {

				requestID, _ := c.Get("request_id")

				// Redact error and path
				redactedErr := security.RedactError(err)
				redactedPath := security.MaskPII(c.Request.URL.Path)

				logger.Log.WithFields(map[string]interface{}{
					"level":      "error",
					"request_id": requestID,
					"path":       redactedPath,
					"error":      redactedErr,
				}).Error("panic recovered")

				c.JSON(http.StatusInternalServerError, gin.H{
					"error": "internal server error",
				})
				c.Abort()
			}
		}()

		c.Next()
	}
}
