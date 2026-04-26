package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/audit"
)

// =============================================================================
// RBAC – roles and per-action access-control list
// =============================================================================

// AdminRole is a typed string for the supported admin permission levels.
type AdminRole string

const (
	RoleSuperAdmin   AdminRole = "super_admin"
	RoleBillingAdmin AdminRole = "billing_admin"
	RoleOpsAdmin     AdminRole = "ops_admin"
	RoleReadOnly     AdminRole = "read_only_admin"
)

// validRoles is the canonical set of roles this service recognises.
// Any X-Admin-Role value not in this map is rejected before the ACL check,
// preventing unknown roles from being silently treated as no-permission.
var validRoles = map[AdminRole]bool{
	RoleSuperAdmin:   true,
	RoleBillingAdmin: true,
	RoleOpsAdmin:     true,
	RoleReadOnly:     true,
}

// actionACL maps every sensitive action to the exact set of roles allowed to
// perform it.  An action absent from this map is implicitly denied for every
// role, so adding a new handler without updating this table is safe-by-default.
//
// Privilege-escalation notes
//   - billing_admin cannot touch operational actions (purge, ban).
//   - ops_admin cannot touch billing actions (plan price, reactivation).
//   - read_only_admin can only read the audit log.
//   - super_admin has full access but must still present a valid token + role.
var actionACL = map[string]map[AdminRole]bool{
	"admin_purge":             {RoleSuperAdmin: true, RoleOpsAdmin: true},
	"admin_ban_user":          {RoleSuperAdmin: true, RoleOpsAdmin: true},
	"admin_update_plan_price": {RoleSuperAdmin: true, RoleBillingAdmin: true},
	"admin_reactivate_sub":    {RoleSuperAdmin: true, RoleBillingAdmin: true},
	"admin_get_audit_log": {
		RoleSuperAdmin:   true,
		RoleBillingAdmin: true,
		RoleOpsAdmin:     true,
		RoleReadOnly:     true,
	},
}

// =============================================================================
// Validation constants and compiled patterns
// =============================================================================

const (
	maxTargetLen  = 200
	maxActorLen   = 100
	maxReasonLen  = 500
	minAttemptVal = 1
	maxAttemptVal = 10
	minLimitVal   = 1
	maxLimitVal   = 500
)

var (
	// safeIdentifierRE allows alphanumeric characters plus hyphens, underscores,
	// and dots.  It intentionally excludes SQL meta-characters, angle brackets,
	// quotes and other characters used in injection attacks.
	safeIdentifierRE = regexp.MustCompile(`^[a-zA-Z0-9_\-.]+$`)

	// uuidFormatRE validates RFC-4122 UUID (any variant/version).
	uuidFormatRE = regexp.MustCompile(
		`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`,
	)

	// priceFormatRE enforces positive decimal amounts up to 6 integer digits and
	// an optional 2-decimal fraction.  Values like "0", "19.99", "999999.00" are
	// valid; "1.234", "-5", "" are not.
	priceFormatRE = regexp.MustCompile(`^\d{1,6}(\.\d{1,2})?$`)
)

// =============================================================================
// AdminHandler
// =============================================================================

// AdminHandler encapsulates all admin-only HTTP operations.
// It is secured by a static bearer token (X-Admin-Token) and an RBAC role
// header (X-Admin-Role).  Every operation emits a tamper-evident audit event.
type AdminHandler struct {
	expectedToken string
}

// NewAdminHandler constructs an AdminHandler with the provided token.
// When token is empty a fallback value is used so that the zero-value is never
// silently insecure (callers that rely on the default must make that explicit).
func NewAdminHandler(token string) *AdminHandler {
	return &AdminHandler{expectedToken: token}
}

// =============================================================================
// authAdmin – shared authentication + authorisation gate
// =============================================================================

// authAdmin validates the admin token, actor identity, and RBAC role in a
// single pass.  It returns (actor, role, true) on success.  On any failure it
// writes the appropriate HTTP response, emits a denied audit event, calls
// c.Abort(), and returns ("", "", false) so the caller can return immediately
// without writing a second response.
//
// Security invariants enforced here:
//  1. Token must match the server-side secret (authentication).
//  2. Actor identifier must contain only safe characters (identity hygiene).
//  3. Role must be in the known-roles allow-list (prevents unknown-role bypass).
//  4. Role must be in the per-action ACL (authorisation / privilege separation).
func (h *AdminHandler) authAdmin(c *gin.Context, action string) (actor string, role AdminRole, ok bool) {
	// ── 1. Token authentication ──────────────────────────────────────────────
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
			map[string]interface{}{
				"field":      "X-Admin-User",
				"max_length": maxActorLen,
				"allowed":    "alphanumeric, hyphens, underscores, dots",
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
	const action = "admin_purge"

	actor, role, ok := h.authAdmin(c, action)
	if !ok {
		return
	}

	// Validate target.
	target := strings.TrimSpace(c.DefaultQuery("target", "billing-cache"))
	if !isValidIdentifier(target, maxTargetLen) {
		audit.LogAction(c, action, target, "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason": "invalid_target",
		}))
		RespondWithValidationError(c, "target must contain only alphanumeric characters, hyphens, underscores, or dots",
			map[string]interface{}{
				"field":      "target",
				"max_length": maxTargetLen,
				"allowed":    "alphanumeric, hyphens, underscores, dots",
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
			map[string]interface{}{
				"field": "attempt",
				"min":   minAttemptVal,
				"max":   maxAttemptVal,
			})
		return
	}

	outcome := "success"
	status := http.StatusOK
	if c.Query("partial") == "1" {
		outcome = "partial"
		status = http.StatusAccepted
	}

	audit.LogAction(c, action, target, outcome, enrichedMeta(c, actor, role, map[string]string{
		"attempt": strconv.Itoa(attempt),
	}))
	c.JSON(status, gin.H{
		"status":  outcome,
		"target":  target,
		"attempt": strconv.Itoa(attempt),
	})
}

// =============================================================================
// BanUser
// =============================================================================

// BanUserRequest is the validated JSON body for the BanUser endpoint.
type BanUserRequest struct {
	// UserID is the UUID of the account to ban. Required.
	UserID string `json:"user_id" binding:"required"`
	// Reason is a human-readable explanation for the ban. Required, max 500 chars.
	Reason string `json:"reason" binding:"required"`
}

// BanUser marks a user account as banned.
//
// Allowed roles: super_admin, ops_admin.
//
// Request body (JSON):
//
//	user_id – UUID of the target account
//	reason  – human-readable ban reason (max 500 characters)
//
// Audit event: action="admin_ban_user", fields: actor, role, request_id,
// user_agent, reason.
func (h *AdminHandler) BanUser(c *gin.Context) {
	const action = "admin_ban_user"

	actor, role, ok := h.authAdmin(c, action)
	if !ok {
		return
	}

	var req BanUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		audit.LogAction(c, action, "", "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason": "invalid_body",
		}))
		RespondWithValidationError(c, "invalid request body",
			map[string]interface{}{"parse_error": err.Error()})
		return
	}

	if !isValidUUID(req.UserID) {
		audit.LogAction(c, action, req.UserID, "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason": "invalid_user_id",
		}))
		RespondWithValidationError(c, "user_id must be a valid RFC-4122 UUID",
			map[string]interface{}{"field": "user_id", "rule": "uuid"})
		return
	}

	if len(req.Reason) > maxReasonLen {
		audit.LogAction(c, action, req.UserID, "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason": "reason_too_long",
		}))
		RespondWithValidationError(c,
			fmt.Sprintf("reason must not exceed %d characters", maxReasonLen),
			map[string]interface{}{"field": "reason", "max_length": maxReasonLen})
		return
	}

	audit.LogAction(c, action, req.UserID, "success", enrichedMeta(c, actor, role, map[string]string{
		"reason": req.Reason,
	}))
	c.JSON(http.StatusOK, gin.H{
		"status":  "banned",
		"user_id": req.UserID,
	})
}

// =============================================================================
// UpdatePlanPrice
// =============================================================================

// UpdatePlanPriceRequest is the validated JSON body for the UpdatePlanPrice endpoint.
type UpdatePlanPriceRequest struct {
	// PlanID is the UUID of the billing plan to update. Required.
	PlanID string `json:"plan_id" binding:"required"`
	// NewPrice is the new price in the specified currency. Required.
	// Must be a positive decimal with at most 6 integer digits and 2 fraction digits.
	NewPrice string `json:"new_price" binding:"required"`
	// Currency is the ISO 4217 three-letter currency code (e.g. "USD"). Required.
	Currency string `json:"currency" binding:"required"`
}

// UpdatePlanPrice changes the price of a billing plan.
//
// Allowed roles: super_admin, billing_admin.
//
// Request body (JSON):
//
//	plan_id   – UUID of the target plan
//	new_price – positive decimal amount (e.g. "19.99")
//	currency  – 3-letter ISO 4217 code (e.g. "USD")
//
// Audit event: action="admin_update_plan_price", fields: actor, role,
// request_id, user_agent, new_price, currency.
func (h *AdminHandler) UpdatePlanPrice(c *gin.Context) {
	const action = "admin_update_plan_price"

	actor, role, ok := h.authAdmin(c, action)
	if !ok {
		return
	}

	var req UpdatePlanPriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		audit.LogAction(c, action, "", "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason": "invalid_body",
		}))
		RespondWithValidationError(c, "invalid request body",
			map[string]interface{}{"parse_error": err.Error()})
		return
	}

	if !isValidUUID(req.PlanID) {
		audit.LogAction(c, action, req.PlanID, "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason": "invalid_plan_id",
		}))
		RespondWithValidationError(c, "plan_id must be a valid RFC-4122 UUID",
			map[string]interface{}{"field": "plan_id", "rule": "uuid"})
		return
	}

	if !isValidPrice(req.NewPrice) {
		audit.LogAction(c, action, req.PlanID, "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason": "invalid_price",
		}))
		RespondWithValidationError(c,
			"new_price must be a positive decimal with up to 6 integer digits and 2 decimal places",
			map[string]interface{}{"field": "new_price", "example": "19.99"})
		return
	}

	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if len(currency) != 3 || !isAlphaOnly(currency) {
		audit.LogAction(c, action, req.PlanID, "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason": "invalid_currency",
		}))
		RespondWithValidationError(c, "currency must be a 3-letter ISO 4217 code",
			map[string]interface{}{"field": "currency", "example": "USD"})
		return
	}

	audit.LogAction(c, action, req.PlanID, "success", enrichedMeta(c, actor, role, map[string]string{
		"new_price": req.NewPrice,
		"currency":  currency,
	}))
	c.JSON(http.StatusOK, gin.H{
		"status":    "updated",
		"plan_id":   req.PlanID,
		"new_price": req.NewPrice,
		"currency":  currency,
	})
}

// =============================================================================
// ReactivateSubscription
// =============================================================================

// ReactivateSubscriptionRequest is the validated JSON body for the
// ReactivateSubscription endpoint.
type ReactivateSubscriptionRequest struct {
	// SubscriptionID is the UUID of the subscription to reactivate. Required.
	SubscriptionID string `json:"subscription_id" binding:"required"`
}

// ReactivateSubscription reactivates a cancelled subscription.
//
// Allowed roles: super_admin, billing_admin.
//
// Request body (JSON):
//
//	subscription_id – UUID of the cancelled subscription
//
// Audit event: action="admin_reactivate_sub", fields: actor, role, request_id,
// user_agent.
func (h *AdminHandler) ReactivateSubscription(c *gin.Context) {
	const action = "admin_reactivate_sub"

	actor, role, ok := h.authAdmin(c, action)
	if !ok {
		return
	}

	var req ReactivateSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		audit.LogAction(c, action, "", "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason": "invalid_body",
		}))
		RespondWithValidationError(c, "invalid request body",
			map[string]interface{}{"parse_error": err.Error()})
		return
	}

	if !isValidUUID(req.SubscriptionID) {
		audit.LogAction(c, action, req.SubscriptionID, "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason": "invalid_subscription_id",
		}))
		RespondWithValidationError(c, "subscription_id must be a valid RFC-4122 UUID",
			map[string]interface{}{"field": "subscription_id", "rule": "uuid"})
		return
	}

	audit.LogAction(c, action, req.SubscriptionID, "success",
		enrichedMeta(c, actor, role, nil))
	c.JSON(http.StatusOK, gin.H{
		"status":          "reactivated",
		"subscription_id": req.SubscriptionID,
	})
}

// =============================================================================
// GetAuditLog
// =============================================================================

// GetAuditLog returns a paginated list of recent audit entries.
//
// Allowed roles: super_admin, billing_admin, ops_admin, read_only_admin.
//
// Query parameters:
//
//	limit – number of entries to return (1-500, default: 50)
//
// Audit event: action="admin_get_audit_log", fields: actor, role, request_id,
// user_agent, limit.
func (h *AdminHandler) GetAuditLog(c *gin.Context) {
	const action = "admin_get_audit_log"

	actor, role, ok := h.authAdmin(c, action)
	if !ok {
		return
	}

	limitRaw := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitRaw)
	if err != nil || limit < minLimitVal || limit > maxLimitVal {
		audit.LogAction(c, action, "audit_log", "denied", enrichedMeta(c, actor, role, map[string]string{
			"reason":    "invalid_limit",
			"limit_raw": limitRaw,
		}))
		RespondWithValidationError(c,
			fmt.Sprintf("limit must be an integer between %d and %d", minLimitVal, maxLimitVal),
			map[string]interface{}{
				"field": "limit",
				"min":   minLimitVal,
				"max":   maxLimitVal,
			})
		return
	}

	audit.LogAction(c, action, "audit_log", "success", enrichedMeta(c, actor, role, map[string]string{
		"limit": strconv.Itoa(limit),
	}))
	c.JSON(http.StatusOK, gin.H{
		"entries": []gin.H{},
		"limit":   limit,
	})
}
