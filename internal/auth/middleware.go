package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const RoleContextKey = "role"

// Extract role (temporary: header-based)
// Later replace with JWT parsing
func ExtractRole(c *gin.Context) Role {
	if v, ok := c.Get(RoleContextKey); ok {
		if r, ok := v.(Role); ok {
			return r
		}
		if s, ok := v.(string); ok {
			return Role(s)
		}
	}
	role := c.GetHeader("X-Role")
	if role == "" {
		return ""
	}
	return Role(role)
}

func RequirePermission(permission Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := ExtractRole(c)

		if role == "" || !HasPermission(role, permission) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "insufficient permissions",
			})
			return
		}

		c.Set(RoleContextKey, role)
		c.Next()
	}
}

