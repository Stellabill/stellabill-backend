package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"stellarbill-backend/internal/auth"
)

// ContextKeySubject is the gin context key under which the JWT subject ("sub") claim is stored.
const ContextKeySubject = "jwt_subject"

func respondAuthError(c *gin.Context, msg string) {
	c.JSON(http.StatusUnauthorized, gin.H{"error": msg})
	c.Abort()
}

// AuthMiddleware returns a Gin handler that enforces JWT bearer-token authentication.
func AuthMiddleware(cache interface{}, jwtSecret string) gin.HandlerFunc {
	var jwksCache *auth.JWKSCache
	if cache != nil {
		if jc, ok := cache.(*auth.JWKSCache); ok {
			jwksCache = jc
		}
	}

	return func(c *gin.Context) {
		fmt.Printf("DEBUG: AuthMiddleware entered for path %s\n", c.Request.URL.Path)
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			respondAuthError(c, "missing authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authorization header must be Bearer token",
			})
			return
		}

		tokenStr := parts[1]

		// 1. Use the JWKSCache to find the correct public key for validation, or fallback to HMAC secret
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if jwksCache != nil {
				// Only use JWKS for RS256
				if _, ok := t.Method.(*jwt.SigningMethodRSA); ok {
					kid, ok := t.Header["kid"].(string)
					if !ok {
						return nil, fmt.Errorf("missing kid in token header")
					}

					// Call GetKey which handles the "Refresh-on-Error" logic
					key, err := jwksCache.GetKey(c.Request.Context(), kid)
					if err != nil {
						return nil, fmt.Errorf("failed to get key from cache: %w", err)
					}

					var rawKey interface{}
					if err := key.Raw(&rawKey); err != nil {
						return nil, fmt.Errorf("failed to get raw key: %w", err)
					}

					return rawKey, nil
				}
			}

			// Fallback: HMAC secret (HS256)
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(jwtSecret), nil
		})

		if err != nil || !token.Valid {
			fmt.Printf("DEBUG: token validation failed: %v\n", err)
			respondAuthError(c, "invalid or expired token")
			return
		}

		// Extract Claims
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid token claims",
			})
			return
		}

		sub, err := claims.GetSubject()
		if err != nil || sub == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "token missing subject claim",
			})
			return
		}

		// Store subject ("sub") for downstream use.
		c.Set(ContextKeySubject, sub)

		// Store roles so that auth.RequirePermission can read them without
		// knowing about JWT internals.
		roles := extractRoles(claims)
		c.Set(auth.RolesContextKey, roles)

		// 3. Tenant ID enforcement
		tenantHeader := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
		tenantClaim := ""
		if v, ok := claims["tenant_id"]; ok {
			if ts, ok := v.(string); ok {
				tenantClaim = strings.TrimSpace(ts)
			}
		} else if v, ok := claims["tenant"]; ok {
			if ts, ok := v.(string); ok {
				tenantClaim = strings.TrimSpace(ts)
			}
		}

		var tenantID string
		if tenantHeader != "" && tenantClaim != "" {
			if tenantHeader != tenantClaim {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "tenant mismatch",
				})
				return
			}
			tenantID = tenantHeader
		} else if tenantHeader != "" {
			tenantID = tenantHeader
		} else if tenantClaim != "" {
			tenantID = tenantClaim
		}

		// Project claims into gin context for downstream handlers
		c.Set("callerID", sub)
		c.Set("tenantID", tenantID)
		
		c.Next()
	}
}

// mapJWTError converts jwt library errors to safe, user-facing messages.
func mapJWTError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return fmt.Errorf("token has expired")
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return fmt.Errorf("token is not yet valid")
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return fmt.Errorf("token signature is invalid")
	case errors.Is(err, jwt.ErrTokenMalformed):
		return fmt.Errorf("token is malformed")
	default:
		return fmt.Errorf("token is invalid")
	}
}

// extractRoles reads the "roles" claim from the token, accepting both a
// []interface{} (JSON array) and a plain string.
func extractRoles(claims jwt.MapClaims) []auth.Role {
	raw, ok := claims["roles"]
	if !ok {
		// Try "role" claim
		if r, ok := claims["role"]; ok {
			if s, ok := r.(string); ok && strings.TrimSpace(s) != "" {
				return []auth.Role{auth.Role(strings.TrimSpace(s))}
			}
		}
		return nil
	}

	switch v := raw.(type) {
	case []interface{}:
		roles := make([]auth.Role, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				roles = append(roles, auth.Role(strings.TrimSpace(s)))
			}
		}
		return roles
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []auth.Role{auth.Role(strings.TrimSpace(v))}
	default:
		return nil
	}
}
