package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const PrincipalKey contextKey = "principal"

// ErrorResponse standardizes auth error output
type ErrorResponse struct {
	Error string `json:"error"`
}

// Config holds JWT requirements with hardened validation settings
type Config struct {
	Secret       []byte
	Issuer       string
	Audience     string
	ClockSkewSec int64  // Maximum allowed clock skew in seconds (default 0, recommended <= 60)
	MaxTokenAge  int64  // Maximum age of token in seconds (additional check beyond exp)
	Algorithm    string // Expected algorithm (default "HS256")
}

// ValidateConfig performs security checks on JWT configuration
func (c *Config) ValidateConfig() error {
	if len(c.Secret) == 0 {
		return errors.New("JWT secret cannot be empty")
	}
	if len(c.Secret) < 32 {
		return errors.New("JWT secret should be at least 32 bytes for security")
	}
	if c.Issuer == "" {
		return errors.New("issuer must be configured")
	}
	if c.Audience == "" {
		return errors.New("audience must be configured")
	}
	if c.Algorithm == "" {
		return errors.New("algorithm must be explicitly configured")
	}
	if c.ClockSkewSec < 0 {
		return errors.New("clock skew cannot be negative")
	}
	if c.ClockSkewSec > 300 { // 5 minutes max
		return errors.New("clock skew too large (max 300 seconds)")
	}
	if c.MaxTokenAge < 0 {
		return errors.New("max token age cannot be negative")
	}
	return nil
}

// Claims represents our custom JWT structure
type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

// JWTMiddleware creates a middleware verifying tokens against the provided config
// with hardened validation for issuer/audience, clock skew, and algorithm handling
func JWTMiddleware(cfg Config) func(http.Handler) http.Handler {
	if err := cfg.ValidateConfig(); err != nil {
		panic(fmt.Sprintf("invalid JWT config: %v", err))
	}

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
			if tokenString == "" {
				respondWithError(w, http.StatusUnauthorized, "token string cannot be empty")
				return
			}

			claims := &Claims{}

			// Parse with custom claims validation
			token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
				// Explicitly validate algorithm to prevent algorithm confusion attacks
				if t.Method.Alg() != cfg.Algorithm {
					return nil, fmt.Errorf("unexpected algorithm: expected %s, got %s", cfg.Algorithm, t.Method.Alg())
				}

				// Ensure only HMAC is used (or whatever was configured)
				if cfg.Algorithm == "HS256" || cfg.Algorithm == "HS384" || cfg.Algorithm == "HS512" {
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, errors.New("token method is not HMAC")
					}
				}

				return cfg.Secret, nil
			})

			if err != nil {
				respondWithError(w, http.StatusUnauthorized, fmt.Sprintf("token parsing failed: %v", err))
				return
			}

			if !token.Valid {
				respondWithError(w, http.StatusUnauthorized, "token is not valid")
				return
			}

			// Hardened validation logic
			if err := validateClaimsStrict(claims, cfg); err != nil {
				respondWithError(w, http.StatusUnauthorized, err.Error())
				return
			}

			// Attach principal to request context
			ctx := context.WithValue(r.Context(), PrincipalKey, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// validateClaimsStrict performs strict validation of JWT claims
func validateClaimsStrict(claims *Claims, cfg Config) error {
	now := time.Now()

	// Validate Issuer (required and must match exactly)
	if claims.Issuer != cfg.Issuer {
		return fmt.Errorf("invalid issuer: expected %q, got %q", cfg.Issuer, claims.Issuer)
	}

	// Validate Audience (required and must contain our audience)
	if !stringInSlice(cfg.Audience, claims.Audience) {
		return fmt.Errorf("invalid audience: required %q not found in %v", cfg.Audience, claims.Audience)
	}

	// Validate ExpiresAt with clock skew tolerance
	if claims.ExpiresAt == nil {
		return errors.New("token expiration claim missing")
	}
	expiryTime := claims.ExpiresAt.Time
	if now.After(expiryTime.Add(time.Duration(cfg.ClockSkewSec) * time.Second)) {
		return fmt.Errorf("token expired at %v (now: %v, allowed skew: %ds)", expiryTime, now, cfg.ClockSkewSec)
	}

	// Validate NotBefore with clock skew tolerance
	if claims.NotBefore != nil {
		notBeforeTime := claims.NotBefore.Time
		if now.Before(notBeforeTime.Add(-time.Duration(cfg.ClockSkewSec) * time.Second)) {
			return fmt.Errorf("token not valid until %v (now: %v, allowed skew: %ds)", notBeforeTime, now, cfg.ClockSkewSec)
		}
	}

	// Validate IssuedAt to detect token age
	if claims.IssuedAt != nil && cfg.MaxTokenAge > 0 {
		issuedTime := claims.IssuedAt.Time
		tokenAge := now.Sub(issuedTime).Seconds()
		if tokenAge > float64(cfg.MaxTokenAge) {
			return fmt.Errorf("token too old: issued %v seconds ago, max age: %ds", int64(tokenAge), cfg.MaxTokenAge)
		}
	}

	// Ensure subject and user_id are present
	if claims.Subject == "" && claims.UserID == "" {
		return errors.New("token missing both subject and user_id claims")
	}

	return nil
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
