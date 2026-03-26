// Package cors provides environment-specific CORS policy profiles for the
// Stellarbill API. In development, a permissive wildcard policy is used for
// ergonomics. In production, only explicitly allowlisted origins are accepted.
package cors

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Profile holds the CORS policy for a given environment.
type Profile struct {
	// AllowedOrigins is the explicit list of permitted origins.
	// A single "*" enables the wildcard (development only).
	AllowedOrigins []string

	// AllowedMethods lists the HTTP methods advertised in preflight responses.
	AllowedMethods []string

	// AllowedHeaders lists the request headers clients may send.
	AllowedHeaders []string

	// AllowCredentials sets Access-Control-Allow-Credentials.
	// Must be false when AllowedOrigins contains "*".
	AllowCredentials bool

	// MaxAge is the preflight cache duration sent via Access-Control-Max-Age.
	MaxAge time.Duration
}

// isWildcard reports whether the profile uses the permissive wildcard origin.
func (p *Profile) isWildcard() bool {
	return len(p.AllowedOrigins) == 1 && p.AllowedOrigins[0] == "*"
}

// allowsOrigin reports whether origin is permitted by this profile.
func (p *Profile) allowsOrigin(origin string) bool {
	if p.isWildcard() {
		return true
	}
	return slices.Contains(p.AllowedOrigins, origin)
}

// DevelopmentProfile returns a permissive policy suitable for local development.
func DevelopmentProfile() *Profile {
	return &Profile{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "Idempotency-Key"},
		AllowCredentials: false, // credentials are incompatible with wildcard
		MaxAge:           0,     // no caching in dev
	}
}

// ProductionProfile returns a strict policy that only allows the given origins.
// origins must be fully-qualified (e.g. "https://app.stellarbill.com").
func ProductionProfile(origins []string) *Profile {
	return &Profile{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "Idempotency-Key"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
}

// ProfileForEnv selects the right profile based on the env string and the
// comma-separated list of allowed origins (used in non-development envs).
func ProfileForEnv(env, rawOrigins string) *Profile {
	if env != "production" && env != "staging" {
		return DevelopmentProfile()
	}
	var origins []string
	for _, o := range strings.Split(rawOrigins, ",") {
		if trimmed := strings.TrimSpace(o); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	if len(origins) == 0 {
		// Fail closed: no origins configured means nothing is allowed.
		return ProductionProfile([]string{})
	}
	return ProductionProfile(origins)
}

// Middleware returns a Gin handler that enforces the given CORS profile.
//
// Security notes:
//   - Wildcard origin (*) is only used in development; credentials are never
//     sent alongside a wildcard to comply with the CORS spec.
//   - In production/staging, requests from unlisted origins receive no
//     Access-Control-Allow-Origin header, causing browsers to block the response.
//   - Preflight responses are cached by the browser for Profile.MaxAge to reduce
//     round-trips without sacrificing security.
//   - The Vary: Origin header is always set so CDNs/proxies cache per-origin.
func Middleware(p *Profile) gin.HandlerFunc {
	methods := strings.Join(p.AllowedMethods, ", ")
	headers := strings.Join(p.AllowedHeaders, ", ")
	maxAge := strconv.Itoa(int(p.MaxAge.Seconds()))

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// Always vary on Origin so intermediate caches don't serve the wrong policy.
		c.Header("Vary", "Origin")

		if origin == "" {
			// Non-browser or same-origin request — skip CORS headers entirely.
			c.Next()
			return
		}

		if !p.allowsOrigin(origin) {
			// Origin not in allowlist — do not set ACAO header.
			// For preflight, return 403 so the browser surfaces a clear error.
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		if p.isWildcard() {
			c.Header("Access-Control-Allow-Origin", "*")
		} else {
			c.Header("Access-Control-Allow-Origin", origin)
		}

		c.Header("Access-Control-Allow-Methods", methods)
		c.Header("Access-Control-Allow-Headers", headers)

		if p.AllowCredentials {
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		if c.Request.Method == http.MethodOptions {
			if p.MaxAge > 0 {
				c.Header("Access-Control-Max-Age", maxAge)
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
