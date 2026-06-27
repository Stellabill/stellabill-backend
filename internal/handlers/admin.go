package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AdminHandler encapsulates admin-only HTTP operations.
type AdminHandler struct {
	expectedToken string
}

// NewAdminHandler constructs an AdminHandler with the provided token.
func NewAdminHandler(token string) *AdminHandler {
	return &AdminHandler{expectedToken: token}
}

// PurgeCache handles cache purge requests. It is a placeholder implementation
// gated on the admin token; full RBAC and audit logging are intentionally out
// of scope for the minimal CI build.
func (h *AdminHandler) PurgeCache(c *gin.Context) {
	if token := c.GetHeader("X-Admin-Token"); token == "" || token != h.expectedToken {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "purged"})
}
