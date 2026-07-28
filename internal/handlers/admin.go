package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// OPAAuthorizer interface for decoupling
type OPAAuthorizer interface {
	Authorize(ctx context.Context, token string) (bool, error)
}

// AdminHandler encapsulates admin-only HTTP operations.
type AdminHandler struct {
	expectedToken string
	authorizer    OPAAuthorizer
}

// NewAdminHandler constructs an AdminHandler with the provided token.
func NewAdminHandler(token string, opts ...interface{}) *AdminHandler {
	h := &AdminHandler{expectedToken: token}
	for _, opt := range opts {
		if auth, ok := opt.(OPAAuthorizer); ok {
			h.authorizer = auth
		}
	}
	return h
}

// PurgeCache handles cache purge requests. It is a placeholder implementation
// gated on the admin token; full RBAC and audit logging are intentionally out
// of scope for the minimal CI build.
func (h *AdminHandler) PurgeCache(c *gin.Context) {
	token := c.GetHeader("X-Admin-Token")
	if token == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if h.authorizer != nil {
		allow, err := h.authorizer.Authorize(c.Request.Context(), token)
		if err != nil || !allow {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
	} else if token != h.expectedToken {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "purged"})
}
