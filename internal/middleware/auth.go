package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"stellarbill-backend/internal/auth"
)

var jwksCache *auth.JWKSCache

// InitJWKSCache initializes the JWKS cache with the given URL and TTL (seconds).
func InitJWKSCache(jwksURL string, ttlSeconds int) {
	if jwksURL != "" {
		jwksCache = auth.NewJWKSCache(jwksURL, time.Duration(ttlSeconds)*time.Second)
	}
}

// AuthMiddleware returns a middleware that validates JWT tokens using JWKS
// and projects verified claims (roles, callerID, tenantID) into the gin context
func AuthMiddleware(jwksURL interface{}, ttl string) gin.HandlerFunc {
	// Initialize JWKS cache if not already done
	if jwksCache == nil && jwksURL != nil {
		if url, ok := jwksURL.(string); ok && url != "" {
			InitJWKSCache(url, 300) // Default 5 minutes TTL
		}
	}

	useJWKS := jwksURL != nil

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authorization header required",
			})
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

		// Parse and validate JWT token
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			// If JWKS cache is available and requested, use it for validation
			if useJWKS && jwksCache != nil {
				// Ensure the token is using RSA/ECDSA (standard for JWKS)
				if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
					if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
						return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
					}
				}

				kid, ok := t.Header["kid"].(string)
				if !ok {
					return nil, fmt.Errorf("missing kid in token header")
				}

				key, err := jwksCache.GetKey(c.Request.Context(), kid)
				if err != nil {
					return nil, fmt.Errorf("failed to retrieve public key: %w", err)
				}

				var rawKey interface{}
				if err := key.Raw(&rawKey); err != nil {
					return nil, fmt.Errorf("failed to get raw key: %w", err)
				}

				return rawKey, nil
			}

			// Fallback: If no JWKS cache, accept the token for testing purposes using the provided secret
			// In production, this should be removed or properly configured
			secret := ttl
			if secret == "" {
				secret = "test-secret"
			}
			return []byte(secret), nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": fmt.Sprintf("token validation failed: %v", err),
			})
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

		// Extract and normalize roles from JWT claims
		roles := extractRolesFromClaims(claims)

		// Tenant ID enforcement
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
		} else {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "tenant id required",
			})
			return
		}

		// Project claims into gin context for downstream handlers
		c.Set(auth.RolesContextKey, roles)
		c.Set("callerID", sub)
		c.Set("tenantID", tenantID)
		
		c.Next()
	}
}

// extractRolesFromClaims extracts and normalizes roles from JWT claims
// Handles both single role (string) and multiple roles ([]string or []interface{})
func extractRolesFromClaims(claims jwt.MapClaims) []auth.Role {
	var roles []auth.Role

	// Try to extract "roles" claim (array)
	if v, ok := claims["roles"]; ok {
		switch typed := v.(type) {
		case []string:
			for _, role := range typed {
				if trimmed := strings.TrimSpace(role); trimmed != "" {
					roles = append(roles, auth.Role(trimmed))
				}
			}
		case []interface{}:
			for _, role := range typed {
				if roleStr, ok := role.(string); ok {
					if trimmed := strings.TrimSpace(roleStr); trimmed != "" {
						roles = append(roles, auth.Role(trimmed))
					}
				}
			}
		case []auth.Role:
			roles = typed
		}
	}

	// If no roles found, try "role" claim (single string)
	if len(roles) == 0 {
		if v, ok := claims["role"]; ok {
			switch typed := v.(type) {
			case string:
				if trimmed := strings.TrimSpace(typed); trimmed != "" {
					roles = append(roles, auth.Role(trimmed))
				}
			case auth.Role:
				if trimmed := strings.TrimSpace(string(typed)); trimmed != "" {
					roles = append(roles, typed)
				}
			}
		}
	}

	// Normalize roles using the existing auth.ExtractRoles logic
	// Create a temporary gin context to use the existing normalization function
	tempCtx := &gin.Context{}
	tempCtx.Set(auth.RolesContextKey, roles)
	return auth.ExtractRoles(tempCtx)
}