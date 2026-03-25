package handlers

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "stellarbill-backend",
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