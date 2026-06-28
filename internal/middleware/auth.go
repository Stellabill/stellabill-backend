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

// InitJWKSCache initializes the JWKS cache with the given URL and TTL.
func InitJWKSCache(jwksURL string, ttlSeconds int) {
	if jwksURL != "" {
		jwksCache = auth.NewJWKSCache(jwksURL, time.Duration(ttlSeconds)*time.Second)
	}
}

// AuthMiddleware validates JWT bearer tokens using JWKS when configured, otherwise
// HS256 with jwtSecret. Projects roles, callerID, and tenantID into the context.
func AuthMiddleware(jwksURL interface{}, jwtSecret string) gin.HandlerFunc {
	if jwksCache == nil && jwksURL != nil {
		if url, ok := jwksURL.(string); ok && url != "" {
			InitJWKSCache(url, 300)
		}
	}

	secret := jwtSecret
	if secret == "" {
		secret = "test-secret"
	}

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing authorization header",
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

		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if jwksCache != nil {
				if _, ok := t.Method.(*jwt.SigningMethodRSA); ok {
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
				if _, ok := t.Method.(*jwt.SigningMethodECDSA); ok {
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
			}

			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(secret), nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
			})
			return
		}

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

		roles := extractRolesFromClaims(claims)

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

		c.Set(auth.RolesContextKey, roles)
		c.Set("callerID", sub)
		c.Set("tenantID", tenantID)

		c.Next()
	}
}

func extractRolesFromClaims(claims jwt.MapClaims) []auth.Role {
	var roles []auth.Role

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

	tempCtx := &gin.Context{}
	tempCtx.Set(auth.RolesContextKey, roles)
	return auth.ExtractRoles(tempCtx)
}
