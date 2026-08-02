package middleware

import (
	"errors"
	"net/http"
	"strings"

	"stellarbill-backend/internal/auth"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// AuthMiddleware validates bearer JWTs and optional admin token fallback.
// When a Redis revocation store is provided, revoked JTIs are denied.
// A bearer token always takes precedence over an admin token header.
func AuthMiddleware(store auth.RevocationStore, jwtSecret string, extras ...interface{}) gin.HandlerFunc {
	adminToken := ""
	failOpen := false
	if len(extras) > 0 {
		if v, ok := extras[0].(string); ok {
			adminToken = v
		}
	}
	if len(extras) > 1 {
		if v, ok := extras[1].(bool); ok {
			failOpen = v
		}
	}

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			if adminToken == "" {
				abortUnauthorized(c, "missing authorization header")
				return
			}
			if token := c.GetHeader("X-Admin-Token"); token == adminToken {
				c.Set(auth.RoleContextKey, auth.RoleAdmin)
				c.Set(auth.RolesContextKey, []auth.Role{auth.RoleAdmin})
				c.Set("callerID", "admin-token")
				if tenantID := c.GetHeader("X-Tenant-ID"); tenantID != "" {
					c.Set("tenantID", tenantID)
				}
				c.Next()
				return
			}
			abortUnauthorized(c, "missing authorization header")
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			abortUnauthorized(c, "invalid authorization format")
			return
		}

		tokenString := parts[1]
		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			abortUnauthorized(c, "invalid or expired token")
			return
		}

		callerID := firstString(claims, "user_id", "sub")
		if callerID == "" {
			abortUnauthorized(c, "missing subject claim")
			return
		}

		tenantID := firstString(claims, "tenant_id", "tenant")
		if headerTenant := c.GetHeader("X-Tenant-ID"); headerTenant != "" {
			if tenantID != "" && tenantID != headerTenant {
				abortUnauthorized(c, "tenant ID mismatch")
				return
			}
			tenantID = headerTenant
		}
		if tenantID == "" {
			abortUnauthorized(c, "missing tenant_id claim")
			return
		}

		if store != nil {
			revoked, err := store.IsRevoked(c.Request.Context(), firstString(claims, "jti"))
			if err != nil {
				if !failOpen {
					abortUnauthorized(c, "unable to validate token revocation")
					return
				}
			} else if revoked {
				abortUnauthorized(c, "revoked token")
				return
			}
		}

		roles := extractRolesFromClaims(claims)
		if len(roles) > 0 {
			roleValues := make([]auth.Role, 0, len(roles))
			for _, role := range roles {
				roleValues = append(roleValues, auth.Role(role))
			}
			c.Set(auth.RolesContextKey, roleValues)
			c.Set(auth.RoleContextKey, roleValues[0])
		}
		c.Set("callerID", callerID)
		c.Set("tenantID", tenantID)

		c.Next()
	}
}

func abortUnauthorized(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": message})
}

func firstString(claims jwt.MapClaims, keys ...string) string {
	for _, key := range keys {
		if val, ok := claims[key]; ok {
			if str, ok := val.(string); ok {
				return str
			}
		}
	}
	return ""
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
	if v, ok := claims["role"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return []string{s}
		}
	}
	return nil
}
