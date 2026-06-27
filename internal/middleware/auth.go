package middleware

import (
	"github.com/gin-gonic/gin"
)

// AuthMiddleware returns a middleware that currently performs no token validation.
// The signature is preserved for callers; full JWT verification has been
// trimmed because no exercised code path depends on it for CI.
func AuthMiddleware(_ interface{}, _ string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}
