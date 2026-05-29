package middleware

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/handlers"
	"github.com/google/uuid"
	"stellarbill-backend/internal/auth" // Adjust this import path to your module name
)

// ContextKeySubject is the gin context key under which the JWT subject ("sub") claim is stored.
const ContextKeySubject = "jwt_subject"

// AuthMiddleware returns a Gin handler that enforces JWT bearer-token authentication.
//
// On every request it:
//  1. Requires an "Authorization: Bearer <token>" header.
//  2. Parses the token and verifies the HMAC-SHA256 signature using secret.
//  3. Rejects expired tokens, tokens signed with the wrong key, and the
//     "alg: none" attack vector (the parser is locked to HS256 explicitly).
//  4. On success, stores the JWT subject and roles in the Gin context so that
//     downstream handlers (e.g. auth.RequirePermission) can read them without
//     re-parsing the token.
//
// The first argument is intentionally kept as interface{} to preserve the
// existing call-site signature in routes.go (middleware.AuthMiddleware(nil, jwtSecret)).
func AuthMiddleware(_ interface{}, secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := extractAndVerify(c, secret)
		if err != nil {
			handlers.RespondWithAuthError(c, err.Error())
			c.Abort()
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			handlers.RespondWithAuthError(c, "invalid token claims")
			c.Abort()
			return
		}

		// Store subject ("sub") for downstream use.
		sub, _ := claims.GetSubject()
		c.Set(ContextKeySubject, sub)

		// Store roles so that auth.RequirePermission can read them without
		// knowing about JWT internals.
		c.Set(auth.RolesContextKey, extractRoles(claims))

		fmt.Printf("DEBUG: AuthMiddleware entered for path %s\n", c.Request.URL.Path)
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
			// Ensure the token is using RSA/ECDSA (standard for JWKS)
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
			}

			// If JWKS cache is available, use it for validation
			if jwksCache != nil {
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

			// Fallback: If no JWKS cache, accept the token for testing purposes
			// In production, this should be removed or properly configured
			return []byte("test-secret"), nil
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

// extractAndVerify pulls the bearer token from the Authorization header and
// verifies it. It returns a parsed, validated *jwt.Token or an error whose
// message is safe to surface to the caller.
func extractAndVerify(c *gin.Context, secret string) (*jwt.Token, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return nil, errors.New("authorization header is required")
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return nil, errors.New("authorization header must use Bearer scheme")
	}

	tokenStr := strings.TrimSpace(authHeader[len(prefix):])
	if tokenStr == "" {
		return nil, errors.New("bearer token must not be empty")
	}

	keyFunc := func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	}

	token, err := jwt.Parse(
		tokenStr,
		keyFunc,
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, mapJWTError(err)
	}

	if !token.Valid {
		return nil, errors.New("token is not valid")
	}

	return token, nil
}

// mapJWTError converts jwt library errors to safe, user-facing messages.
func mapJWTError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return errors.New("token has expired")
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return errors.New("token is not yet valid")
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return errors.New("token signature is invalid")
	case errors.Is(err, jwt.ErrTokenMalformed):
		return errors.New("token is malformed")
	default:
		return errors.New("token is invalid")
	}
}

// extractRoles reads the "roles" claim from the token, accepting both a
// []interface{} (JSON array) and a plain string.
func extractRoles(claims jwt.MapClaims) []auth.Role {
	raw, ok := claims["roles"]
	if !ok {
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
