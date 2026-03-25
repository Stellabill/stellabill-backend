package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const RoleContextKey = "role"

// Extract role (temporary: header-based)
// Later replace with JWT parsing
func ExtractRole(c *gin.Context) Role {
	role := c.GetHeader("X-Role")
	if role == "" {
		return ""
	}
	return Role(role)
}

func RequirePermission(permission Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := ExtractRole(c)

		if role == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing role",
			})
			return
		}

		if !HasPermission(role, permission) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "forbidden",
			})
			return
		}

		c.Set(RoleContextKey, role)
		c.Next()
	}
}
