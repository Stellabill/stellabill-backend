package startup

import (
	"net/http"
	"sync"
	"time"

	"stellabill-backend/internal/config"

	"github.com/gin-gonic/gin"
)

// DiagnosticsHandler serves the machine-readable diagnostics endpoint.
// It re-runs startup checks on demand so operators can triage live issues.
type DiagnosticsHandler struct {
	cfg       config.Config
	db        DBPinger
	migStatus MigrationStatusFunc
	startedAt time.Time

	mu            sync.RWMutex
	cachedResults []CheckResult
	cachedAt      time.Time
}

const cacheTTL = 5 * time.Second

// NewDiagnosticsHandler creates a handler that re-runs checks on each request.
func NewDiagnosticsHandler(cfg config.Config, db DBPinger, migStatus MigrationStatusFunc) *DiagnosticsHandler {
	return &DiagnosticsHandler{
		cfg:       cfg,
		db:        db,
		migStatus: migStatus,
		startedAt: time.Now(),
	}
}

// Handle is the gin handler for GET /api/admin/diagnostics.
func (d *DiagnosticsHandler) Handle(c *gin.Context) {
	results := d.getResults()

	resp := DiagnosticsResponse{
		Status:        OverallStatus(results),
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		UptimeSeconds: time.Since(d.startedAt).Seconds(),
		Checks:        results,
	}

	code := http.StatusOK
	if resp.Status != "ready" {
		code = http.StatusServiceUnavailable
	}

	c.JSON(code, resp)
}

// getResults returns cached results if fresh, otherwise re-runs checks.
func (d *DiagnosticsHandler) getResults() []CheckResult {
	d.mu.RLock()
	if d.cachedResults != nil && time.Since(d.cachedAt) < cacheTTL {
		results := d.cachedResults
		d.mu.RUnlock()
		return results
	}
	d.mu.RUnlock()

	results := RunChecks(d.cfg, d.db, d.migStatus)

	d.mu.Lock()
	d.cachedResults = results
	d.cachedAt = time.Now()
	d.mu.Unlock()

	return results
}

