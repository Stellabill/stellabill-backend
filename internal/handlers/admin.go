package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/cache"
)

// AdminHandler encapsulates admin-only operations (secured via static token).
// Inject cache.Purgeable instances at construction time via NewAdminHandler so
// PurgeCache can actually invalidate live cache state rather than returning a
// placeholder response.
type AdminHandler struct {
	expectedToken string
	purgeables    []cache.Purgeable
}

// NewAdminHandler builds an admin handler.
//   - token: the expected value of the X-Admin-Token request header.
//     If empty, defaults to "change-me-admin-token".
//   - purgeables: zero or more cache namespaces to flush on POST /api/admin/purge.
//     Pass each CachedPlanRepo / CachedSubscriptionRepo here.
func NewAdminHandler(token string, purgeables ...cache.Purgeable) *AdminHandler {
	if token == "" {
		token = "change-me-admin-token"
	}
	return &AdminHandler{expectedToken: token, purgeables: purgeables}
}

// namespaceSummary holds the per-namespace result included in the purge response.
type namespaceSummary struct {
	Namespace     string `json:"namespace"`
	KeysPurged    int    `json:"keys_purged"`
	CountersReset bool   `json:"counters_reset"`
	Error         string `json:"error,omitempty"`
}

// purgeResponse is the JSON body returned by a successful PurgeCache call.
type purgeResponse struct {
	Status          string             `json:"status"`
	TotalKeysPurged int                `json:"total_keys_purged"`
	Namespaces      []namespaceSummary `json:"namespaces"`
	Timestamp       time.Time          `json:"timestamp"`
}

// PurgeCache invalidates all active cache entries managed by the registered
// cache namespaces, resets hit/miss counters, and returns a detailed summary.
//
// Behaviour:
//   - Idempotent: repeated calls on an already-empty cache return 200 with
//     total_keys_purged = 0 and no error.
//   - Concurrent-safe: each Purgeable is responsible for its own locking;
//     the handler collects results independently per namespace.
//   - Partial failure: if any namespace returns an error the HTTP status is 202
//     and the "error" field is set on the affected namespace summary. Other
//     namespaces that succeeded are still reported correctly.
//   - Auth: a missing or wrong X-Admin-Token header returns 401 immediately
//     without touching any cache state.
func (h *AdminHandler) PurgeCache(c *gin.Context) {
	target := c.DefaultQuery("target", "billing-cache")
	attempt := c.DefaultQuery("attempt", "1")
	actor := c.GetHeader("X-Admin-User")
	if actor == "" {
		actor = "unknown-admin"
	}

	// --- Auth check ---
	token := c.GetHeader("X-Admin-Token")
	if token != h.expectedToken {
		audit.LogAction(c, "admin_purge", c.FullPath(), "denied", map[string]string{
			"reason": "invalid_token",
		})
		RespondWithError(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "invalid admin token")
		c.Abort()
		return
	}

	ctx := c.Request.Context()

	// --- Flush every registered namespace ---
	summaries := make([]namespaceSummary, 0, len(h.purgeables))
	totalKeys := 0
	hasError := false

	for _, p := range h.purgeables {
		ns := namespaceSummary{Namespace: p.Namespace()}

		n, err := p.Flush(ctx)
		if err != nil {
			ns.Error = err.Error()
			hasError = true
		} else {
			ns.KeysPurged = n
			totalKeys += n
		}

		// Always reset metrics regardless of flush outcome so counters do not
		// accumulate stale data from before the attempted purge.
		p.ResetMetrics()
		ns.CountersReset = true

		summaries = append(summaries, ns)
	}

	// --- Determine outcome ---
	// "partial" if any namespace errored OR if the caller explicitly set ?partial=1
	// (the ?partial=1 param is retained for backward compatibility with existing
	// audit/demo tests that simulate partial operations).
	auditOutcome := "success"
	httpStatus := http.StatusOK
	respStatus := "purged"

	if hasError || c.Query("partial") == "1" {
		auditOutcome = "partial"
		httpStatus = http.StatusAccepted
		respStatus = "partial"
	}

	audit.LogAction(c, "admin_purge", target, auditOutcome, map[string]string{
		"attempt":     attempt,
		"keys_purged": strconv.Itoa(totalKeys),
	})

	c.JSON(httpStatus, purgeResponse{
		Status:          respStatus,
		TotalKeysPurged: totalKeys,
		Namespaces:      summaries,
		Timestamp:       time.Now().UTC(),
	})
}
