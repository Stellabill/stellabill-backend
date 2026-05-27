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
		audit.LogAction(c, action, c.FullPath(), "denied", map[string]string{
			"reason": "invalid_token",
		})
		RespondWithError(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "invalid admin token")
		c.Abort()
		return
	}

	// ── 2. Actor identity validation ─────────────────────────────────────────
	actor = strings.TrimSpace(c.GetHeader("X-Admin-User"))
	if actor == "" {
		actor = "unknown-admin"
	} else if !isValidIdentifier(actor, maxActorLen) {
		audit.LogAction(c, action, c.FullPath(), "denied", map[string]string{
			"reason": "invalid_actor",
		})
		RespondWithValidationError(c, "X-Admin-User contains invalid characters or exceeds maximum length",
			[]validation.FieldError{
				{Field: "X-Admin-User", Message: fmt.Sprintf("max_length: %d, allowed: alphanumeric, hyphens, underscores, dots", maxActorLen)},
			})
		c.Abort()
		return
	}

	// ── 3. Role existence check ───────────────────────────────────────────────
	rawRole := strings.TrimSpace(c.GetHeader("X-Admin-Role"))
	role = AdminRole(rawRole)
	if !validRoles[role] {
		audit.LogAction(c, action, c.FullPath(), "denied", map[string]string{
			"actor":  actor,
			"reason": "unknown_role",
		})
		RespondWithError(c, http.StatusForbidden, ErrorCodeForbidden,
			fmt.Sprintf("unknown admin role %q; valid roles: super_admin, billing_admin, ops_admin, read_only_admin", rawRole))
		c.Abort()
		return
	}

	// ── 4. Per-action ACL check ───────────────────────────────────────────────
	if allowed := actionACL[action]; !allowed[role] {
		audit.LogAction(c, action, c.FullPath(), "denied", map[string]string{
			"actor":  actor,
			"role":   rawRole,
			"reason": "insufficient_permissions",
		})
		RespondWithError(c, http.StatusForbidden, ErrorCodeForbidden,
			fmt.Sprintf("role %q does not have permission to perform %q", rawRole, action))
		c.Abort()
		return
	}

	return actor, role, true
}

// =============================================================================
// enrichedMeta – mandatory audit metadata builder
// =============================================================================

// enrichedMeta returns the baseline set of metadata fields that every admin
// audit event must carry:
//
//   - actor      – the human identity that initiated the call
//   - role       – the RBAC role used for this request
//   - request_id – value of X-Request-ID header (or context key "requestID")
//   - user_agent – value of the User-Agent header
//
// Additional key-value pairs from `extra` are merged in, with extra values
// winning on collision so that individual handlers can override defaults.
func enrichedMeta(c *gin.Context, actor string, role AdminRole, extra map[string]string) map[string]string {
	meta := map[string]string{
		"actor":      actor,
		"role":       string(role),
		"user_agent": c.GetHeader("User-Agent"),
		"request_id": resolveRequestID(c),
	}
	for k, v := range extra {
		meta[k] = v
	}
	return meta
}

// resolveRequestID extracts a correlation/request-id from the request for use
// in audit metadata.  It checks the X-Request-ID header first, then falls back
// to the "requestID" Gin context key set by upstream request-id middleware.
func resolveRequestID(c *gin.Context) string {
	if v := strings.TrimSpace(c.GetHeader("X-Request-ID")); v != "" {
		return v
	}
	if v, ok := c.Get("requestID"); ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// =============================================================================
// Validation helpers
// =============================================================================

// isValidIdentifier returns true when s is non-empty, contains only characters
// matched by safeIdentifierRE, and does not exceed maxLen runes.
func isValidIdentifier(s string, maxLen int) bool {
	if utf8.RuneCountInString(s) > maxLen {
		return false
	}
	return safeIdentifierRE.MatchString(s)
}

// isValidUUID returns true when s matches the canonical UUID format.
func isValidUUID(s string) bool {
	return uuidFormatRE.MatchString(s)
}

// isValidPrice returns true when s is a positive decimal amount matching
// priceFormatRE (up to 6 integer digits, optional 2-digit fraction).
func isValidPrice(s string) bool {
	return priceFormatRE.MatchString(s)
}

// parseAttempt converts raw to an integer and validates it is within the
// [minAttemptVal, maxAttemptVal] range.
func parseAttempt(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("attempt must be a positive integer, got %q", raw)
	}
	if n < minAttemptVal || n > maxAttemptVal {
		return 0, fmt.Errorf("attempt must be between %d and %d, got %d", minAttemptVal, maxAttemptVal, n)
	}
	return n, nil
}

// isAlphaOnly returns true when every rune in s is an ASCII letter (A-Z / a-z)
// and s is non-empty.  Used for currency code validation.
func isAlphaOnly(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return false
		}
	}
	return true
}

// =============================================================================
// PurgeCache
// =============================================================================

// PurgeCache evicts a named cache target.
//
// Allowed roles: super_admin, ops_admin.
//
// Query parameters:
//
//	target  – name of the cache to purge (default: "billing-cache")
//	attempt – retry counter 1-10 (default: "1")
//	partial – set to "1" for a partial purge (returns 202 Accepted)
//
// Audit event: action="admin_purge", fields: actor, role, request_id,
// user_agent, attempt.
func (h *AdminHandler) PurgeCache(c *gin.Context) {
	if token := c.GetHeader("X-Admin-Token"); token == "" || token != h.expectedToken {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// Validate target.
	target := strings.TrimSpace(c.DefaultQuery("target", "billing-cache"))
	if !isValidIdentifier(target, maxTargetLen) {
		audit.LogAction(c, action, target, "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason": "invalid_target",
		}))
		RespondWithValidationError(c, "target must contain only alphanumeric characters, hyphens, underscores, or dots",
			[]validation.FieldError{
				{Field: "target", Message: fmt.Sprintf("max_length: %d, allowed: alphanumeric, hyphens, underscores, dots", maxTargetLen)},
			})
		return
	}

	// Validate attempt.
	attemptRaw := c.DefaultQuery("attempt", "1")
	attempt, err := parseAttempt(attemptRaw)
	if err != nil {
		audit.LogAction(c, action, target, "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason":      "invalid_attempt",
			"attempt_raw": attemptRaw,
		}))
		RespondWithValidationError(c, err.Error(),
			[]validation.FieldError{
				{Field: "attempt", Message: fmt.Sprintf("range: [%d, %d]", minAttemptVal, maxAttemptVal)},
			})
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
