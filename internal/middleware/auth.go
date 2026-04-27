package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"stellarbill-backend/internal/auth"
)

type ErrorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

func respondAuthError(c *gin.Context, message string) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	traceID := c.GetString("traceID")
	if traceID == "" {
		traceID = uuid.New().String()
	}

	envelope := ErrorEnvelope{
		Code:    "UNAUTHORIZED",
		Message: message,
		TraceID: traceID,
	}
	c.AbortWithStatusJSON(http.StatusUnauthorized, envelope)
}

// AuthMiddleware creates a Gin middleware for JWT authentication.
// It supports both JWKS (asynchronous key rotation) and static secrets (HS256).
// If jwksCache is nil, it falls back to using the provided secret string.
func AuthMiddleware(jwksCache *auth.JWKSCache, staticSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			respondAuthError(c, "authorization header required")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			respondAuthError(c, "authorization header must be Bearer token")
			return
		}

		tokenStr := parts[1]

		var keyFunc jwt.Keyfunc
		if jwksCache != nil {
			keyFunc = func(t *jwt.Token) (interface{}, error) {
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
		} else {
			keyFunc = func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return []byte(staticSecret), nil
			}
		}

		token, err := jwt.Parse(tokenStr, keyFunc)
		if err != nil || !token.Valid {
			respondAuthError(c, fmt.Sprintf("token validation failed: %v", err))
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			respondAuthError(c, "invalid token claims")
			return
		}

		sub, err := claims.GetSubject()
		if err != nil || sub == "" {
			respondAuthError(c, "token missing subject claim")
			return
		}

		// Tenant ID enforcement
		tenantHeader := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
		tenantClaim := ""
		if v, ok := claims["tenant"]; ok {
			if ts, ok := v.(string); ok {
				tenantClaim = strings.TrimSpace(ts)
			}
		}

		var tenantID string
		if tenantHeader != "" && tenantClaim != "" {
			if tenantHeader != tenantClaim {
				respondAuthError(c, "tenant mismatch")
				return
			}
			tenantID = tenantHeader
		} else if tenantHeader != "" {
			tenantID = tenantHeader
		} else if tenantClaim != "" {
			tenantID = tenantClaim
		} else {
			// For testing or development, we might allow missing tenant if not strictly required
			// But for hardening, we should enforce it if possible.
			// Let's assume it's required for now.
			respondAuthError(c, "tenant id required")
			return
		}

		c.Set("callerID", sub)
		c.Set("tenantID", tenantID)
		c.Next()
	}
}
