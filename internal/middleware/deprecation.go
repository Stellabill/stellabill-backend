package middleware

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// LegacyAPISunsetEnv is the environment variable carrying the sunset date
// advertised on deprecated (/api but not /api/v1) routes. The value may be an
// RFC3339 timestamp, an RFC1123 HTTP date, or a quoted version of either.
const LegacyAPISunsetEnv = "LEGACY_API_SUNSET"

const (
	deprecationHeaderCmd = "true"
	deprecationLinkRel   = "successor-version"
)

// DeprecationHeaders returns middleware that marks legacy /api routes as
// deprecated using structured headers:
//
//   - Deprecation: true
//   - Sunset: parsed from LEGACY_API_SUNSET (omitted when unset or invalid)
//   - Link: </api/v1PATH>; rel="successor-version"
//
// /api/v1 routes are never marked. Headers are written before the handler
// runs so they survive 4xx aborts and panics recovered by the Recovery
// middleware.
func DeprecationHeaders() gin.HandlerFunc {
	sunset := parseLegacySunset(os.Getenv(LegacyAPISunsetEnv))

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/api/v1/") {
			c.Next()
			return
		}

		c.Header("Deprecation", deprecationHeaderCmd)
		if sunset != "" {
			c.Header("Sunset", sunset)
		}
		c.Header("Link", "</api/v1"+strings.TrimPrefix(path, "/api")+">; rel=\""+deprecationLinkRel+"\"")
		c.Next()
	}
}

// DeprecatedHandler is an alias of DeprecationHeaders kept for per-route
// registration alongside r.Use(DeprecationHeaders()).
func DeprecatedHandler() gin.HandlerFunc {
	return DeprecationHeaders()
}

// parseLegacySunset parses the LEGACY_API_SUNSET value into a valid HTTP
// date. An empty or unparseable value returns "" so the Sunset header is
// omitted rather than advertised with a broken date.
func parseLegacySunset(raw string) string {
	if raw == "" {
		return ""
	}

	raw = strings.Trim(raw, `"`)
	for _, layout := range []string{http.TimeFormat, time.RFC3339, time.RFC1123, time.RFC1123Z} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format(http.TimeFormat)
		}
	}
	return ""
}
