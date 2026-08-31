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
	"crypto/subtle"
	"net/http"

	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/security"

	"github.com/gin-gonic/gin"
)

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

// AdminHandler encapsulates admin-only HTTP operations.
type AdminHandler struct {
	expectedToken string
	lockout       *security.LockoutTracker
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

// PurgeCache handles cache purge requests.
func (h *AdminHandler) PurgeCache(c *gin.Context) {
	if token := c.GetHeader("X-Admin-Token"); token == "" || token != h.expectedToken {
		audit.LogAction(c, "admin_purge", "cache", "denied", map[string]string{"reason": "invalid_token"})
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	audit.LogAction(c, "admin_purge", "cache", "success", map[string]string{"status": "purged"})
	c.JSON(http.StatusOK, gin.H{"status": "purged"})
}
