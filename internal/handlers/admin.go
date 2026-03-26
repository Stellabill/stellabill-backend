package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/audit"
)

// AdminHandler encapsulates admin-only operations (secured via static token for now).
type AdminHandler struct {
	expectedToken string
}

// NewAdminHandler builds an admin handler with the expected token.
func NewAdminHandler(token string) *AdminHandler {
	if token == "" {
		token = "change-me-admin-token"
	}
	return &AdminHandler{expectedToken: token}
}

// PurgeCache is a representative sensitive action used for audit coverage.
func (h *AdminHandler) PurgeCache(c *gin.Context) {
	target := c.DefaultQuery("target", "billing-cache")
	attempt := c.DefaultQuery("attempt", "1")
	actor := c.GetHeader("X-Admin-User")
	if actor == "" {
		actor = "unknown-admin"
	}

	token := c.GetHeader("X-Admin-Token")
	if token != h.expectedToken {
		audit.LogAction(c, "admin_purge", target, "denied", map[string]string{
			"attempt": attempt,
			"reason":  "invalid_token",
		})
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid admin token"})
		return
	}

	outcome := "success"
	status := http.StatusOK
	if c.Query("partial") == "1" {
		outcome = "partial"
		status = http.StatusAccepted
	}
	audit.LogAction(c, "admin_purge", target, outcome, map[string]string{
		"attempt": attempt,
	})
	c.JSON(status, gin.H{"status": outcome, "target": target, "attempt": attempt})
}
