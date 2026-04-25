package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ErrorEnvelope for auth errors
type ErrorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
}

// respondAuthError is a helper to respond with auth errors in the standard envelope format
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

// AuthMiddleware validates the Authorization header (Bearer JWT).
// On success it sets "callerID" in the Gin context and calls c.Next().
// On failure it aborts with 401 and a JSON error body.
func AuthMiddleware(jwtSecret string) gin.HandlerFunc {
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
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(jwtSecret), nil
		}, jwt.WithValidMethods([]string{"HS256", "HS384", "HS512"}))

		if err != nil || !token.Valid {
			msg := "invalid or expired token"
			if err != nil {
				msg = fmt.Sprintf("token validation failed: %v", err)
			}
			respondAuthError(c, msg)
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

		// Tenant ID enforcement.
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
			respondAuthError(c, "tenant id required")
			return
		}

		c.Set("callerID", sub)
		c.Set("tenantID", tenantID)
		c.Next()
	}
}
