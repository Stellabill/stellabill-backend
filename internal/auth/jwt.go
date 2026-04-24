package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const PrincipalKey contextKey = "principal"

// ErrorResponse standardizes auth error output
type ErrorResponse struct {
	Error string `json:"error"`
}

// Config holds JWT requirements
type Config struct {
	Secret   []byte
	Issuer   string
	Audience string
}


// JWTMiddleware creates a middleware verifying tokens against the provided config
func JWTMiddleware(cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				respondWithError(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			// Expecting "Bearer <token>"
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				respondWithError(w, http.StatusUnauthorized, "invalid authorization format")
				return
			}

			tokenString := parts[1]
			claims := &Claims{}

			token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
				// Validate the signing algorithm
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}
				return cfg.Secret, nil
			})

			if err != nil || !token.Valid {
				respondWithError(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}

			// Validate Issuer and Audience if configured
			if cfg.Issuer != "" && claims.Issuer != cfg.Issuer {
				respondWithError(w, http.StatusUnauthorized, "invalid issuer")
				return
			}
			if cfg.Audience != "" && !stringInSlice(cfg.Audience, claims.Audience) {
				respondWithError(w, http.StatusUnauthorized, "invalid audience")
				return
			}

			// Attach principal to request context
			ctx := context.WithValue(r.Context(), PrincipalKey, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetPrincipal safely extracts the user ID from the context in downstream handlers
func GetPrincipal(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(PrincipalKey).(string)
	return val, ok
}

// respondWithError ensures standardized JSON output for auth failures
func respondWithError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
