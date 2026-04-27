package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/maintenance"
)

var (
	MaintenanceBlockedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_blocked_maintenance_total",
			Help: "Total number of HTTP requests blocked by maintenance mode",
		},
		[]string{"route", "method"},
	)
)

func MaintenanceMode() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !maintenance.IsActive() {
			c.Next()
			return
		}

		// Allow safe reads as defined by policy
		method := c.Request.Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}

		// Let admin endpoints bypass to disable/enable maintenance mode
		path := c.FullPath()
		if strings.HasPrefix(path, "/api/admin/maintenance") {
			c.Next()
			return
		}

		route := path
		if route == "" {
			route = "unknown"
		}
		MaintenanceBlockedTotal.WithLabelValues(route, method).Inc()

		audit.LogAction(c, "maintenance_mode_block", path, "blocked", map[string]string{
			"reason": "maintenance mode is active",
		})

		requestID, _ := c.Get(RequestIDKey)
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"error":      "Service is currently in maintenance mode. Only safe reads are allowed.",
			"code":       "MAINTENANCE_MODE",
			"request_id": requestID,
		})
	}
}
