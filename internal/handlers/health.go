package handlers

import (
	"context"
	"math"
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

// ReadinessHandler checks if the service is ready
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

		statusCode := http.StatusOK
		if overallStatus == StatusDegraded || overallStatus == StatusUnavailable {
			statusCode = http.StatusServiceUnavailable
		}

		c.JSON(statusCode, resp)
	}
}

// checkDatabase implements Timeout and Bounded Retry policies
func checkDatabase(db DBPinger) string {
	if os.Getenv("DATABASE_URL") == "" {
		return "not_configured"
	}
	if db == nil {
		return "down"
	}

	var lastErr error

	// IMPLEMENTATION: Bounded Retry Loop
	for i := 0; i < MaxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), MaxDatabaseTimeout)
		
		lastErr = db.PingContext(ctx)
		cancel() // Release context resources immediately

		if lastErr == nil {
			return "up"
		}

		// If not the last attempt, wait before retrying (Exponential Backoff)
		if i < MaxRetries-1 {
			backoff := time.Duration(math.Pow(2, float64(i))) * InitialBackoff
			time.Sleep(backoff)
		}
	}

	// Determine final failure state
	if lastErr == context.DeadlineExceeded {
		return "timeout"
	}
	return "down"
}

func deriveOverallStatus(deps map[string]string) string {
	for _, status := range deps {
		if status == "down" || status == "timeout" {
			return StatusDegraded
		}
	}
	return StatusReady
}