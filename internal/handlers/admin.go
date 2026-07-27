package handlers

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/cache"

	"github.com/gin-gonic/gin"
)

// namespaceSummary is the per-namespace result of a Flush call.
type namespaceSummary struct {
	Namespace     string `json:"namespace"`
	KeysPurged    int    `json:"keys_purged"`
	CountersReset bool   `json:"counters_reset"`
	Error         string `json:"error,omitempty"`
}

// purgeResponse is the JSON body returned by the purge endpoint.
type purgeResponse struct {
	Status          string            `json:"status"`
	TotalKeysPurged int               `json:"total_keys_purged"`
	Namespaces      []namespaceSummary `json:"namespaces"`
	Timestamp       time.Time         `json:"timestamp"`
}

// AdminHandler encapsulates admin-only HTTP operations.
type AdminHandler struct {
	expectedToken string
	purgeables    []cache.Purgeable
	mu            sync.Mutex // guards concurrent purge requests
}

// NewAdminHandler constructs an AdminHandler with the provided token
// and optional cache.Purgeable backends that will be flushed on purge.
func NewAdminHandler(token string, purgeables ...cache.Purgeable) *AdminHandler {
	return &AdminHandler{
		expectedToken: token,
		purgeables:    purgeables,
	}
}

// PurgeCache handles cache purge requests. It iterates over all registered
// Purgeable backends, calls Flush on each, and returns a summary.
// If all backends flush successfully, it returns 200 OK.
// If any backend fails, it returns 202 Accepted (partial success).
// If auth fails, it returns 401 Unauthorized.
func (h *AdminHandler) PurgeCache(c *gin.Context) {
	if token := c.GetHeader("X-Admin-Token"); token == "" || token != h.expectedToken {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx := c.Request.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	adminUser := c.GetHeader("X-Admin-User")

	// Collect query params for audit metadata
	auditMeta := make(map[string]string)
	for k, vals := range c.Request.URL.Query() {
		if len(vals) > 0 {
			auditMeta[k] = vals[0]
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var (
		results   []namespaceSummary
		totalKeys int
		hasErrors bool
	)

	for _, p := range h.purgeables {
		ns := p.Namespace()
		keys, err := p.Flush(ctx)
		p.ResetMetrics()

		summary := namespaceSummary{
			Namespace:     ns,
			KeysPurged:    keys,
			CountersReset: true,
		}

		totalKeys += keys

		if err != nil {
			hasErrors = true
			summary.Error = err.Error()
		}

		results = append(results, summary)
	}

	status := "success"
	httpStatus := http.StatusOK
	auditOutcome := "success"

	if hasErrors {
		status = "partial"
		httpStatus = http.StatusAccepted
		auditOutcome = "partial"
	}

	auditMeta["keys_purged"] = strconv.Itoa(totalKeys)
	if adminUser != "" {
		auditMeta["admin_user"] = adminUser
	}

	audit.LogAction(c, "admin_purge", "cache", auditOutcome, auditMeta)

	c.JSON(httpStatus, purgeResponse{
		Status:          status,
		TotalKeysPurged: totalKeys,
		Namespaces:      results,
		Timestamp:       time.Now().UTC(),
	})
}
