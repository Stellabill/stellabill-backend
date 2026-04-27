package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"stellabill-backend/internal/startup"
)

func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "stellabill-backend",
	})
}

func ReadinessHandler(db startup.DBPinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := "ready"
		code := http.StatusOK

		if db == nil {
			status = "unavailable"
			code = http.StatusServiceUnavailable
		} else {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			defer cancel()
			if err := db.PingContext(ctx); err != nil {
				status = "unavailable"
				code = http.StatusServiceUnavailable
			}
		}

		c.JSON(code, gin.H{
			"status":    status,
			"service":   "stellabill-backend",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}
}
