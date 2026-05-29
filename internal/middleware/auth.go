package middleware

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/handlers"
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
