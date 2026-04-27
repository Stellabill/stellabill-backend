package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"stellabill-backend/internal/auth"
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

	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error":    message,
		"code":     "UNAUTHORIZED",
		"trace_id": traceID,
	})
}

// AuthMiddleware validates the Authorization header (Bearer JWT).
// On success it sets "callerID" in the Gin context and calls c.Next().
// On failure it aborts with 401 and a JSON error body.
func AuthMiddleware(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			respondAuthError(c, "missing authorization header")
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			respondAuthError(c, "invalid authorization header format")
			return
		}

		tokenStr := parts[1]
		var claims auth.Claims
		token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(jwtSecret), nil
		}, jwt.WithValidMethods([]string{"HS256", "HS384", "HS512"}))

		if err != nil || !token.Valid {
			respondAuthError(c, "invalid or expired token")
			return
		}

		// User identifier
		sub := claims.Subject
		if sub == "" {
			sub = claims.UserID
		}
		if sub == "" {
			respondAuthError(c, "token missing user identifier")
			return
		}

		// Tenant ID enforcement.
		tenantHeader := strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
		tenantClaim := strings.TrimSpace(claims.Tenant)

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
			// If role is present and not admin, tenant is required.
			// If role is missing, we let it pass to let permission guards return 403.
			if claims.Role != "" && claims.Role != string(auth.RoleAdmin) {
				respondAuthError(c, "tenant id required")
				return
			}
			if claims.Role == string(auth.RoleAdmin) {
				tenantID = "system"
			}
		}

		c.Set("callerID", sub)
		if claims.Role != "" {
			c.Set("role", claims.Role)
		}

		if tenantID != "" {
			c.Set("tenantID", tenantID)
		}
		c.Next()
	}
}

