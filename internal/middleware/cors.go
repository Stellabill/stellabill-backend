package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS creates a strict CORS middleware enforcing an origin allow-list.
func CORS(env string, allowedOriginsRaw string) gin.HandlerFunc {
	isProdEnv := env == "production" || env == "staging"

	allowedOrigins := make(map[string]bool)
	rawList := strings.Split(allowedOriginsRaw, ",")
	for _, o := range rawList {
		o = strings.TrimSpace(strings.ToLower(o))
		if o != "" {
			// In production/staging, never allow wildcard
			if isProdEnv && o == "*" {
				continue
			}
			allowedOrigins[o] = true
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		c.Header("Vary", "Origin")

		// Not a cross-origin request
		if origin == "" {
			c.Next()
			return
		}

		lowerOrigin := strings.ToLower(origin)
		isAllowed := false

		if isProdEnv {
			isAllowed = allowedOrigins[lowerOrigin]
		} else {
			// Development mode
			if len(allowedOrigins) == 0 || allowedOrigins["*"] {
				isAllowed = true
			} else {
				isAllowed = allowedOrigins[lowerOrigin]
			}
		}

		if !isAllowed {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		allowOriginHeader := origin
		if !isProdEnv && (len(allowedOrigins) == 0 || allowedOrigins["*"]) {
			allowOriginHeader = "*"
		}

		c.Header("Access-Control-Allow-Origin", allowOriginHeader)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")

		if allowOriginHeader != "*" {
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
