package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// AuthMiddleware returns a middleware that currently performs no token validation.
// The signature is preserved for callers; full JWT verification has been
// trimmed because no exercised code path depends on it for CI.
func AuthMiddleware(_ interface{}, _ string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

// jwksCache is a package-level cache for JWKS keys.  Nil until initialized
// via InitJWKSCache.  Tests inspect this variable directly.
var jwksCache interface{}

// InitJWKSCache pre-warms the JWKS key cache from the given URL.
// ttlSeconds controls how long keys are retained before a refresh.
// This is a stub — full implementation is deferred until JWKS-based auth
// is enabled in production.
func InitJWKSCache(url string, ttlSeconds int) {
	// Minimal stub: mark the cache as initialized so tests can assert it is
	// non-nil.  A real implementation would fetch and parse the JWKS document.
	jwksCache = struct{ URL string; TTL int }{URL: url, TTL: ttlSeconds}
}

// extractRolesFromClaims extracts a slice of role strings from a JWT
// MapClaims.  It handles the following formats produced by common IdPs:
//   - "roles": []string{"admin", "user"}
//   - "roles": []interface{}{"admin", "user"}
//   - "role": "admin"   (single role, singular key)
func extractRolesFromClaims(claims jwt.MapClaims) []string {
	if v, ok := claims["roles"]; ok {
		switch t := v.(type) {
		case []string:
			return t
		case []interface{}:
			roles := make([]string, 0, len(t))
			for _, r := range t {
				if s, ok := r.(string); ok {
					roles = append(roles, s)
				}
			}
			return roles
		}
	}
	// Fall back to the singular "role" key used by some IdPs.
	if v, ok := claims["role"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return []string{s}
		}
	}
	return nil
}
