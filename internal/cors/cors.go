// Package cors provides environment-specific CORS policy profiles for the
// Stellarbill API. In development, a permissive wildcard policy is used for
// ergonomics. In production, only explicitly allowlisted origins are accepted.
//
// Security guarantees:
//   - Wildcard (*) origins are blocked in production/staging environments
//   - Credentials are never sent with wildcard origins (CORS spec violation)
//   - Origin reflection is only performed for explicitly allowlisted origins
//   - Invalid or malformed origins are rejected without CORS headers
//   - Preflight requests from disallowed origins return 403 Forbidden
//   - All responses include Vary: Origin for proper cache behavior
package cors

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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

// Validate checks that the profile configuration is secure and spec-compliant.
func (p *Profile) Validate() error {
	if p == nil {
		return fmt.Errorf("profile cannot be nil")
	}

	// Check wildcard + credentials violation
	if p.isWildcard() && p.AllowCredentials {
		return fmt.Errorf("credentials cannot be enabled with wildcard origin (CORS spec violation)")
	}

	// Credentials with an empty allowlist is semantically wrong: no ACAO header
	// will ever be emitted, so the flag can never take effect. Catch this early
	// so the misconfiguration is visible rather than silently inert.
	if len(p.AllowedOrigins) == 0 && p.AllowCredentials {
		return fmt.Errorf("credentials cannot be enabled when AllowedOrigins is empty (no origin will ever be reflected)")
	}

	// Validate origin formats
	for _, origin := range p.AllowedOrigins {
		if origin == "*" {
			continue
		}
		if err := validateOriginFormat(origin); err != nil {
			return fmt.Errorf("invalid origin %q: %w", origin, err)
		}
	}

	// Check for duplicate origins
	seen := make(map[string]bool)
	for _, origin := range p.AllowedOrigins {
		if seen[origin] {
			return fmt.Errorf("duplicate origin %q in allowlist", origin)
		}
		seen[origin] = true
	}

	return nil
}

// validateOriginFormat ensures an origin is properly formatted
func validateOriginFormat(origin string) error {
	if origin == "" {
		return fmt.Errorf("origin cannot be empty")
	}

	parsed, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme == "" {
		return fmt.Errorf("must include scheme (e.g., https://)")
	}

	if parsed.Host == "" {
		return fmt.Errorf("must include host")
	}

	// Origins should not have path, query, or fragment
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("must not include path")
	}
	if parsed.RawQuery != "" {
		return fmt.Errorf("must not include query parameters")
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("must not include fragment")
	}

	return nil
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
//
// AllowCredentials is set to true only when at least one origin is provided;
// an empty allowlist means no origin is ever reflected so credentials would
// never accompany a wildcard, but we disable the flag explicitly to keep the
// Profile semantically consistent and to satisfy Validate().
func ProductionProfile(origins []string) *Profile {
	return &Profile{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "Idempotency-Key"},
		AllowCredentials: len(origins) > 0, // never true for fail-closed empty list
		MaxAge:           12 * time.Hour,
	}
}

// ProfileForEnv selects the right profile based on the env string and the
// comma-separated list of allowed origins (used in non-development envs).
// Returns an error-wrapped profile if validation fails.
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
	
	profile := ProductionProfile(origins)

	// Validate the profile before returning. On failure we log a structured
	// error so operators can see exactly which origin failed validation, then
	// return an empty-allowlist profile (fail closed). This should normally be
	// caught by config-layer validation at startup before we ever reach here.
	if err := profile.Validate(); err != nil {
		slog.Error("cors: invalid origin configuration; failing closed with empty allowlist",
			"error", err,
			"env", env,
			"raw_origins", rawOrigins,
		)
		return ProductionProfile([]string{})
	}

	return profile
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
//   - Malformed or suspicious origins are rejected without CORS headers.
//   - Origin reflection only occurs for explicitly allowlisted origins.
func Middleware(p *Profile) gin.HandlerFunc {
	// Catch misconfiguration at wiring time, not at the first request.
	// Callers must validate the Profile (via Validate or ProfileForEnv) before
	// registering the middleware.
	if p == nil {
		panic("cors: Middleware called with nil Profile; call ProfileForEnv or validate at startup")
	}

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

		// Validate origin format before checking allowlist. Browsers always send
		// a well-formed Origin, so a malformed value indicates a crafted/proxied
		// request. Reject preflights with 403; let simple requests pass without
		// any CORS headers (the browser will still block the response).
		if !p.isWildcard() && origin != "*" {
			if err := validateOriginFormat(origin); err != nil {
				if c.Request.Method == http.MethodOptions {
					c.AbortWithStatus(http.StatusForbidden)
					return
				}
				c.Next()
				return
			}
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

		// Origin is allowed — set the response origin header.
		if p.isWildcard() {
			c.Header("Access-Control-Allow-Origin", "*")
		} else {
			// Reflect the exact request origin (already validated against the
			// allowlist above) rather than echoing a constant string, so that
			// multi-origin profiles work correctly with credentials.
			c.Header("Access-Control-Allow-Origin", origin)
		}

		if p.AllowCredentials {
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		// Access-Control-Allow-Methods and Access-Control-Allow-Headers are
		// preflight-response headers per the Fetch spec (§3.2.3). Sending them
		// on every simple/actual request is non-standard, bloats payloads, and
		// unnecessarily exposes the full allowed method/header surface.
		if c.Request.Method == http.MethodOptions {
			c.Header("Access-Control-Allow-Methods", methods)
			c.Header("Access-Control-Allow-Headers", headers)
			if p.MaxAge > 0 {
				c.Header("Access-Control-Max-Age", maxAge)
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
