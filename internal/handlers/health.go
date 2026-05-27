package handlers

import (
	"context"
	"database/sql"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Health status constants
const (
	StatusHealthy      = "healthy"
	StatusDegraded     = "degraded"
	StatusUnhealthy    = "unhealthy"
	ServiceName        = "stellarbill-backend"
	DefaultHTTPTimeout = 5 * time.Second
	DefaultDBTimeout   = 3 * time.Second
	DefaultQueueCheck  = 3 * time.Second
)

// Dependency check configuration
const (
	MaxRetries       = 2
	InitialBackoff   = 100 * time.Millisecond
	MaxDatabaseTimeout = 3 * time.Second
)

// DBPinger defines the interface for database connectivity checks
type DBPinger interface {
	PingContext(ctx context.Context) error
}

// OutboxHealther defines the interface for outbox/queue health checks
type OutboxHealther interface {
	Health() error
	GetStats() (map[string]interface{}, error)
}

// HTTPClientHealther defines the interface for external API health checks
type HTTPClientHealther interface {
	Ping(ctx context.Context) error
}

// HealthResponse represents the structure of health check responses
type HealthResponse struct {
	Status       string                 `json:"status"`
	Service      string                 `json:"service"`
	Timestamp    string                 `json:"timestamp"`
	Dependencies map[string]interface{} `json:"dependencies"`
	Version      string                 `json:"version,omitempty"`
}

// DependencyHealth holds the health status of a single dependency
type DependencyHealth struct {
	Status  string        `json:"status"`
	Message string        `json:"message,omitempty"`
	Latency string        `json:"latency,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// HealthChecker encapsulates all dependency health checks
type HealthChecker struct {
	db     DBPinger
	outbox OutboxHealther
	mu     sync.RWMutex
}

// NewHealthChecker creates a new health checker with dependencies
func NewHealthChecker(db DBPinger, outbox OutboxHealther) *HealthChecker {
	return &HealthChecker{
		db:     db,
		outbox: outbox,
	}
}

// LivenessProbe returns a simple liveness check (application is running)
// Used by Kubernetes liveness probes to restart unhealthy pods
func (h *Handler) LivenessProbe(c *gin.Context) {
	// Simple check: service is running and responding
	// Does not check dependencies (no cascading failures)
	response := HealthResponse{
		Status:    StatusHealthy,
		Service:   ServiceName,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Dependencies: map[string]interface{}{
			"note": "liveness probe - application is running",
		},
	}
	c.JSON(http.StatusOK, response)
}

// ReadinessProbe returns readiness status (ready to handle requests)
// Used by Kubernetes readiness probes to route traffic only to ready pods
// Checks critical dependencies; degraded status means traffic is redirected
func (h *Handler) ReadinessProbe(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	checker := NewHealthChecker(h.getDatabase(), h.getOutboxHealther())
	deps := checker.checkAllDependencies(ctx)

	overallStatus := deriveOverallStatus(deps)
	statusCode := http.StatusOK
	if overallStatus == StatusDegraded || overallStatus == StatusUnhealthy {
		statusCode = http.StatusServiceUnavailable
	}

	response := HealthResponse{
		Status:       overallStatus,
		Service:      ServiceName,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		Dependencies: deps,
	}

	c.JSON(statusCode, response)
}

// HealthDetails returns comprehensive health information for operators
// Includes all dependency details and metrics; not for critical routing decisions
func (h *Handler) HealthDetails(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	checker := NewHealthChecker(h.getDatabase(), h.getOutboxHealther())
	deps := checker.checkAllDependencies(ctx)

	overallStatus := deriveOverallStatus(deps)

	response := HealthResponse{
		Status:       overallStatus,
		Service:      ServiceName,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		Dependencies: deps,
		Version:      os.Getenv("VERSION"), // Set via build flags or environment
	}

	c.JSON(http.StatusOK, response)
}

// checkAllDependencies performs health checks on all dependencies
func (hc *HealthChecker) checkAllDependencies(ctx context.Context) map[string]interface{} {
	deps := make(map[string]interface{})
	results := make(chan struct {
		key   string
		value interface{}
	}, 2)

	// Check database with timeout
	go func() {
		results <- struct {
			key   string
			value interface{}
		}{
			key:   "database",
			value: hc.checkDatabase(ctx),
		}
	}()

	// Check outbox/queue with timeout
	go func() {
		results <- struct {
			key   string
			value interface{}
		}{
			key:   "outbox",
			value: hc.checkOutbox(ctx),
		}
	}()

	remaining := 2
	for remaining > 0 {
		select {
		case res := <-results:
			deps[res.key] = res.value
			remaining--
		case <-ctx.Done():
			// Timeout - mark remaining checks as timeout and return current results
			if _, ok := deps["database"]; !ok {
				deps["database"] = map[string]interface{}{
					"status":  "timeout",
					"message": "database check exceeded timeout",
				}
			}
			if _, ok := deps["outbox"]; !ok {
				deps["outbox"] = map[string]interface{}{
					"status":  "timeout",
					"message": "outbox check exceeded timeout",
				}
			}
			return deps
		}
	}

	return deps
}

// checkDatabase checks database connectivity with retry logic
func (hc *HealthChecker) checkDatabase(ctx context.Context) interface{} {
	if hc.db == nil {
		return DependencyHealth{
			Status:  "not_configured",
			Message: "database client not initialized",
		}
	}

	if os.Getenv("DATABASE_URL") == "" {
		return DependencyHealth{
			Status:  "not_configured",
			Message: "DATABASE_URL not set",
		}
	}

	var lastErr error
	var latency time.Duration

	// Bounded retry loop with exponential backoff
	for attempt := 0; attempt < MaxRetries; attempt++ {
		// Create a bounded context for this attempt
		attemptCtx, cancel := context.WithTimeout(ctx, MaxDatabaseTimeout)

		start := time.Now()
		lastErr = hc.db.PingContext(attemptCtx)
		latency = time.Since(start)
		cancel()

		if lastErr == nil {
			return DependencyHealth{
				Status:  StatusHealthy,
				Latency: latency.String(),
			}
		}

		// If this isn't the last attempt and context isn't cancelled, retry with backoff
		if attempt < MaxRetries-1 {
			select {
			case <-ctx.Done():
				// Parent context cancelled, stop retrying
				return DependencyHealth{
					Status:  StatusDegraded,
					Message: ctx.Err().Error(),
					Latency: latency.String(),
				}
			case <-time.After(time.Duration(math.Pow(2, float64(attempt))) * InitialBackoff):
				// Backoff period complete, try again
			}
		}
	}

	// Determine failure reason for final status
	if lastErr == context.DeadlineExceeded {
		return DependencyHealth{
			Status:  StatusDegraded,
			Message: "database connection timeout - may be overloaded or network issue",
			Latency: latency.String(),
		}
	}

	if lastErr == sql.ErrConnDone {
		return DependencyHealth{
			Status:  StatusUnhealthy,
			Message: "database connection closed unexpectedly",
			Latency: latency.String(),
		}
	}

	return DependencyHealth{
		Status:  StatusDegraded,
		Message: "database unreachable: " + lastErr.Error(),
		Latency: latency.String(),
	}
}

// checkOutbox checks outbox/queue health
func (hc *HealthChecker) checkOutbox(ctx context.Context) interface{} {
	if hc.outbox == nil {
		return DependencyHealth{
			Status:  "not_configured",
			Message: "outbox manager not initialized",
		}
	}

	// Use a timeout context for the outbox check
	ctx, cancel := context.WithTimeout(ctx, DefaultQueueCheck)
	defer cancel()

	start := time.Now()
	err := hc.outbox.Health()
	latency := time.Since(start)

	if err == nil {
		// Optionally include queue stats if available
		stats, _ := hc.outbox.GetStats()
		return DependencyHealth{
			Status:  StatusHealthy,
			Latency: latency.String(),
			Details: stats,
		}
	}

	if ctx.Err() == context.DeadlineExceeded {
		return DependencyHealth{
			Status:  StatusDegraded,
			Message: "outbox health check timeout",
			Latency: latency.String(),
		}
	}

	return DependencyHealth{
		Status:  StatusDegraded,
		Message: "outbox unhealthy: " + err.Error(),
		Latency: latency.String(),
	}
}

// deriveOverallStatus determines overall service status from dependencies
// Rules:
// - If any critical dependency is unhealthy, service is unhealthy
// - If any dependency is degraded, service is degraded
// - Otherwise, service is healthy
func deriveOverallStatus(deps map[string]interface{}) string {
	hasUnhealthy := false
	hasDegraded := false

	for _, depValue := range deps {
		var status string

		// Handle both DependencyHealth struct and map representations
		switch dep := depValue.(type) {
		case DependencyHealth:
			status = dep.Status
		case map[string]interface{}:
			if s, ok := dep["status"].(string); ok {
				status = s
			}
		}

		if status == StatusUnhealthy {
			hasUnhealthy = true
		}
		if status == StatusDegraded {
			hasDegraded = true
		}
	}

	if hasUnhealthy {
		return StatusUnhealthy
	}
	if hasDegraded {
		return StatusDegraded
	}
	return StatusHealthy
}

// Helper methods for Handler to provide dependencies
func (h *Handler) getDatabase() DBPinger {
	// This will be set via dependency injection in main.go
	// For now, return nil - will be wired in during initialization
	if db, ok := h.Database.(DBPinger); ok {
		return db
	}
	return nil
}

func (h *Handler) getOutboxHealther() OutboxHealther {
	// This will be set via dependency injection in main.go
	if outbox, ok := h.Outbox.(OutboxHealther); ok {
		return outbox
	}
	return nil
}