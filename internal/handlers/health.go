package handlers

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/outbox"
)

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "stellarbill-backend",
	}

	// Check outbox health if available
	if globalOutboxManager != nil {
		if err := globalOutboxManager.Health(); err != nil {
			status["status"] = "degraded"
			status["outbox"] = gin.H{
				"status": "unhealthy",
				"error":  err.Error(),
			}
		} else {
			stats, err := globalOutboxManager.GetStats()
			if err == nil {
				status["outbox"] = stats
			}
		}
	}

	c.JSON(http.StatusOK, status)
}

// OutboxStats returns detailed outbox statistics
func OutboxStats(c *gin.Context) {
	if globalOutboxManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Outbox manager not available",
		})
		return
	}

	stats, err := globalOutboxManager.GetStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// PublishTestEvent publishes a test event for development/testing
func PublishTestEvent(c *gin.Context) {
	if globalOutboxManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Outbox manager not available",
		})
		return
	}

	// Get event type from query parameter
	eventType := c.Query("type")
	if eventType == "" {
		eventType = "test.event"
	}

	// Create test event data
	eventData := gin.H{
		"message":     "This is a test event",
		"timestamp":   gin.H{"$date": gin.H{"$numberLong": strconv.FormatInt(c.Request.Context().Value("timestamp").(int64), 10)}},
		"request_id":  c.GetHeader("X-Request-ID"),
		"user_agent":  c.GetHeader("User-Agent"),
		"ip_address": c.ClientIP(),
	}

	// Publish the event
	service := globalOutboxManager.GetService()
	err := service.PublishEvent(c.Request.Context(), eventType, eventData, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "Test event published successfully",
		"event_type": eventType,
	})
}

// --------------------
// READINESS HANDLER
// --------------------

// ReadinessHandler checks if the service is ready (dependencies included)
func ReadinessHandler(db DBPinger) gin.HandlerFunc {
	return func(c *gin.Context) {

		deps := make(map[string]string)

		dbStatus := checkDatabase(db)
		deps["database"] = dbStatus

		overallStatus := deriveOverallStatus(deps)

		resp := HealthResponse{
			Status:       overallStatus,
			Service:      ServiceName,
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
			Dependencies: deps,
		}

		// Map status to HTTP code
		statusCode := http.StatusOK
		if overallStatus == StatusDegraded {
			statusCode = http.StatusServiceUnavailable
		}
		if overallStatus == StatusUnavailable {
			statusCode = http.StatusServiceUnavailable
		}

		c.JSON(statusCode, resp)
	}
}

// --------------------
// DATABASE CHECK
// --------------------

func checkDatabase(db DBPinger) string {

	// If DATABASE_URL not set → not configured
	if os.Getenv("DATABASE_URL") == "" {
		return "not_configured"
	}

	// If DB instance not injected
	if db == nil {
		return "down"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := db.PingContext(ctx)
	if err != nil {
		// Check if timeout
		if ctx.Err() == context.DeadlineExceeded {
			return "timeout"
		}
		return "down"
	}

	return "up"
}

// --------------------
// STATUS DERIVATION
// --------------------

func deriveOverallStatus(deps map[string]string) string {
	hasFailure := false

	for _, status := range deps {
		switch status {
		case "down", "timeout":
			hasFailure = true
		}
	}

	if hasFailure {
		return StatusDegraded
	}

	return StatusReady
}