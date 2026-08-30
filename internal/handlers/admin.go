// Package handlers implements HTTP request handlers for the Stellabill API.
//
// # Admin login lockout
//
// The AdminHandler.Login endpoint uses an exponential backoff lockout to
// rate-limit failed admin authentication attempts.  Each failure for a given
// source IP + account name doubles the lockout duration from 1s up to a
// maximum of 15 minutes.
//
// # Lockout reset
//
// A successful login for the key (source, account) immediately clears its
// lockout state via LockoutTracker.Reset.  Operators can also force a reset
// by restarting the server (state is in-memory) or by calling
// LockoutTracker.Reset programmatically.  There is no admin API to
// unilaterally clear another user's lockout — that is intentional so that
// a compromised admin token cannot be used to suppress rate-limit alerts.
package handlers

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"time"

	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/cache"
	"stellarbill-backend/internal/security"

	"github.com/gin-gonic/gin"
)

// defaultAdminToken is used when no explicit admin token is configured.
const defaultAdminToken = "change-me-admin-token"

// AuditActionAdminPurge is the audit action recorded for cache purge requests.
const AuditActionAdminPurge = "admin_purge"

// AdminLoginRequest is the expected payload for the admin login endpoint.
// Credentials can also be supplied via X-Admin-User and X-Admin-Token headers.
type AdminLoginRequest struct {
	Username string `json:"username" form:"username"`
	Token    string `json:"token" form:"token"`
}

// AdminLoginResponse is returned by the admin login endpoint.
// LockoutDuration is non-zero only when the request was rate-limited.
type AdminLoginResponse struct {
	Status          string `json:"status"`
	LockoutDuration int    `json:"lockout_duration_seconds,omitempty"`
}

// purgeResponse is the JSON envelope returned by PurgeCache.
type purgeResponse struct {
	Status          string             `json:"status"`
	TotalKeysPurged int                `json:"total_keys_purged"`
	Namespaces      []namespaceSummary `json:"namespaces"`
	Timestamp       time.Time          `json:"timestamp"`
}

// namespaceSummary reports the outcome for a single purgeable namespace.
type namespaceSummary struct {
	Namespace     string `json:"namespace"`
	KeysPurged    int    `json:"keys_purged"`
	CountersReset bool   `json:"counters_reset"`
	Error         string `json:"error,omitempty"`
}

// AdminHandler encapsulates admin-only HTTP operations.
type AdminHandler struct {
	expectedToken string
	lockout       *security.LockoutTracker
	purgeables    []cache.Purgeable
}

// NewAdminHandler constructs an AdminHandler with the provided token.
// Optional purgeables and lockout tracker are accepted for backward
// compatibility; the variadic signature allows injecting both.
func NewAdminHandler(token string, rest ...interface{}) *AdminHandler {
	h := &AdminHandler{
		expectedToken: token,
		lockout:       security.NewLockoutTracker(),
	}
	for _, r := range rest {
		switch v := r.(type) {
		case *security.LockoutTracker:
			h.lockout = v
		case cache.Purgeable:
			h.purgeables = append(h.purgeables, v)
		}
	}
	return h
}

// Login authenticates an admin user with exponential lockout protection.
// It accepts credentials via JSON body or query parameters.
// On success, the lockout counter is reset.
// On failure, the attempt is recorded with exponential backoff up to 15 min.
func (h *AdminHandler) Login(c *gin.Context) {
	var req AdminLoginRequest
	if err := c.ShouldBind(&req); err != nil {
		req = h.extractCredentialsFromHeaders(c)
	}

	if req.Username == "" || req.Token == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "username and token are required"})
		return
	}

	source := c.ClientIP()
	account := req.Username

	if h.lockout.IsLocked(source, account) {
		rem := h.lockout.LockoutRemaining(source, account)
		audit.LogAction(c, audit.ActionAdminLogin, "admin_login", "lockout",
			map[string]string{"source": source, "account": account, "reason": "rate_limited"})
		c.AbortWithStatusJSON(http.StatusTooManyRequests, AdminLoginResponse{
			Status:          "rate_limited",
			LockoutDuration: int(rem.Seconds()),
		})
		return
	}

	expected := []byte(h.expectedToken)
	provided := []byte(req.Token)
	valid := len(expected) > 0 && subtle.ConstantTimeCompare(expected, provided) == 1

	if !valid {
		h.lockout.RecordFailure(source, account)
		audit.LogAction(c, audit.ActionAdminLogin, "admin_login", "denied",
			map[string]string{"source": source, "account": account, "reason": "invalid_credentials"})
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	h.lockout.Reset(source, account)
	audit.LogAction(c, audit.ActionAdminLogin, "admin_login", "success",
		map[string]string{"source": source, "account": account})
	c.JSON(http.StatusOK, AdminLoginResponse{Status: "authenticated"})
}

// extractCredentialsFromHeaders falls back to HTTP headers when the request
// body cannot be parsed as AdminLoginRequest.
func (h *AdminHandler) extractCredentialsFromHeaders(c *gin.Context) AdminLoginRequest {
	return AdminLoginRequest{
		Username: c.GetHeader("X-Admin-User"),
		Token:    c.GetHeader("X-Admin-Token"),
	}
}

// PurgeCache handles cache purge requests. Each registered purgeable
// namespace is flushed; a failure in any namespace yields an HTTP 202 with
// status "partial" while the remaining namespaces are still purged. The
// X-Admin-Token header must match the configured token (or the built-in
// default when none is set).
func (h *AdminHandler) PurgeCache(c *gin.Context) {
	expected := h.expectedToken
	if expected == "" {
		expected = defaultAdminToken
	}

	token := c.GetHeader("X-Admin-Token")
	target := c.DefaultQuery("target", "cache")
	metadata := map[string]string{}
	if attempt := c.Query("attempt"); attempt != "" {
		metadata["attempt"] = attempt
	}

	if token == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(token)) != 1 {
		audit.LogAction(c, AuditActionAdminPurge, target, "denied", metadata)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	resp := purgeResponse{
		Status:     "purged",
		Timestamp:  time.Now().UTC(),
		Namespaces: []namespaceSummary{},
	}
	outcome := "success"
	code := http.StatusOK

	for _, p := range h.purgeables {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		keys, err := p.Flush(ctx)
		cancel()

		sum := namespaceSummary{
			Namespace:     p.Namespace(),
			KeysPurged:    keys,
			CountersReset: true,
		}
		if err != nil {
			sum.Error = err.Error()
		}
		resp.TotalKeysPurged += keys
		p.ResetMetrics()
		resp.Namespaces = append(resp.Namespaces, sum)
	}

	if c.Query("partial") == "1" {
		outcome = "partial"
		resp.Status = "partial"
		code = http.StatusAccepted
	} else {
		for _, ns := range resp.Namespaces {
			if ns.Error != "" {
				outcome = "partial"
				resp.Status = "partial"
				code = http.StatusAccepted
				break
			}
		}
	}

	metadata["keys_purged"] = strconv.Itoa(resp.TotalKeysPurged)
	audit.LogAction(c, AuditActionAdminPurge, target, outcome, metadata)
	c.JSON(code, resp)
}
