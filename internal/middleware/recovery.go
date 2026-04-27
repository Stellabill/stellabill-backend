package middleware

import (
	"net/http"

	"stellabill-backend/internal/logger"

	"github.com/gin-gonic/gin"
)

func RecoveryLogger() gin.HandlerFunc {
	return func(c *gin.Context) {

		defer func() {
			if err := recover(); err != nil {

				requestID, _ := c.Get("request_id")

				logger.Log.WithFields(map[string]interface{}{
					"level":      "error",
					"request_id": requestID,
					"path":       c.Request.URL.Path,
					"error":      err,
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

