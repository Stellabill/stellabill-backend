package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var defaultAllowHeaders = []string{
	"Content-Type",
	"Authorization",
	"Idempotency-Key",
	"X-Request-ID",
	"X-Admin-Token",
	"X-Stellabill-Date",
	"X-Stellabill-Request-ID",
	"X-Stellabill-Signature",
}

func buildAllowHeaders(requestedHeaders string) string {
	seen := make(map[string]struct{})
	allowHeaders := make([]string, 0, len(defaultAllowHeaders)+8)

	addHeader := func(header string) {
		header = strings.TrimSpace(header)
		if header == "" {
			return
		}
		key := strings.ToLower(header)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		allowHeaders = append(allowHeaders, header)
	}

	for _, header := range defaultAllowHeaders {
		addHeader(header)
	}

	for _, header := range strings.Split(requestedHeaders, ",") {
		addHeader(header)
	}

	return strings.Join(allowHeaders, ", ")
}

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

		c.Header("Vary", "Origin, Access-Control-Request-Headers")
		allowHeaders := buildAllowHeaders(c.GetHeader("Access-Control-Request-Headers"))

		// Not a cross-origin request. However, treat OPTIONS in non-prod as
		// a preflight and short-circuit even when Origin header is absent
		// (tests and some clients may send bare OPTIONS). In production we
		// require an explicit Origin to avoid loosening security.
		if origin == "" {
			if c.Request.Method == http.MethodOptions && !isProdEnv && (len(allowedOrigins) == 0 || allowedOrigins["*"]) {
				// respond as wildcard-allowed preflight in dev
				c.Header("Access-Control-Allow-Origin", "*")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				c.Header("Access-Control-Allow-Headers", allowHeaders)
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
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
		c.Header("Access-Control-Allow-Headers", allowHeaders)

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
