package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
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
	ClockSkewSec int64      // Maximum allowed clock skew in seconds (default 0, recommended <= 60)
	MaxTokenAge  int64      // Maximum age of token in seconds (additional check beyond exp)
	Algorithm    string     // Expected algorithm (default "HS256")
	JWKS         *JWKSCache // Optional JWKS cache for public key validation
}

// JWTClaims represents our custom JWT structure.
type JWTClaims struct {
	UserID     string   `json:"user_id"`
	Email      string   `json:"email,omitempty"`
	Role       string   `json:"role,omitempty"`
	Roles      []string `json:"roles,omitempty"`
	MerchantID string   `json:"merchant_id,omitempty"`
	jwt.RegisteredClaims
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
			if strings.TrimSpace(parts[1]) == "" {
				respondWithError(w, http.StatusUnauthorized, "invalid authorization format")
				return
			}

			tokenString := parts[1]
			claims := &JWTClaims{}

			// Parse with custom claims validation
			token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
				// Explicitly validate algorithm to prevent algorithm confusion attacks
				if t.Method.Alg() != cfg.Algorithm {
					return nil, fmt.Errorf("unexpected algorithm: expected %s, got %s", cfg.Algorithm, t.Method.Alg())
				}

				// If JWKS is configured, use it to get the public key
				if cfg.JWKS != nil {
					kid, ok := t.Header["kid"].(string)
					if !ok {
						return nil, errors.New("missing kid in token header")
					}
					key, err := cfg.JWKS.GetKey(r.Context(), kid)
					if err != nil {
						return nil, fmt.Errorf("failed to retrieve public key: %w", err)
					}
					var rawKey interface{}
					if err := key.Raw(&rawKey); err != nil {
						return nil, fmt.Errorf("failed to get raw key: %w", err)
					}
					return rawKey, nil
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

// GinJWTMiddleware creates a Gin-compatible middleware verifying tokens
func GinJWTMiddleware(cfg Config) func(*gin.Context) {
	if err := cfg.ValidateConfig(); err != nil {
		panic(fmt.Sprintf("invalid JWT config: %v", err))
	}

	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			respondWithErrorGin(c, http.StatusUnauthorized, "missing authorization header")
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			respondWithErrorGin(c, http.StatusUnauthorized, "invalid authorization format")
			return
		}

		tokenString := parts[1]
		if tokenString == "" {
			respondWithErrorGin(c, http.StatusUnauthorized, "token string cannot be empty")
			return
		}

		claims := &JWTClaims{}

		token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
			// Explicitly validate algorithm
			if t.Method.Alg() != cfg.Algorithm {
				return nil, fmt.Errorf("unexpected algorithm: expected %s, got %s", cfg.Algorithm, t.Method.Alg())
			}

			// If JWKS is configured, use it to get the public key
			if cfg.JWKS != nil {
				kid, ok := t.Header["kid"].(string)
				if !ok {
					return nil, errors.New("missing kid in token header")
				}
				key, err := cfg.JWKS.GetKey(c.Request.Context(), kid)
				if err != nil {
					return nil, fmt.Errorf("failed to retrieve public key: %w", err)
				}
				var rawKey interface{}
				if err := key.Raw(&rawKey); err != nil {
					return nil, fmt.Errorf("failed to get raw key: %w", err)
				}
				return rawKey, nil
			}

			// Fallback to static secret (HMAC)
			return cfg.Secret, nil
		})

		if err != nil {
			respondWithErrorGin(c, http.StatusUnauthorized, fmt.Sprintf("token parsing failed: %v", err))
			return
		}

		if !token.Valid {
			respondWithErrorGin(c, http.StatusUnauthorized, "token is not valid")
			return
		}

		if err := validateClaimsStrict(claims, cfg); err != nil {
			respondWithErrorGin(c, http.StatusUnauthorized, err.Error())
			return
		}

		// Set common context keys for Gin
		c.Set(string(PrincipalKey), claims.UserID)
		c.Set("user_id", claims.UserID)
		c.Set("callerID", claims.UserID) // For backward compatibility
		c.Set("merchant_id", claims.MerchantID)
		c.Set("tenantID", claims.MerchantID) // For backward compatibility
		c.Set(RoleContextKey, string(claims.Role))
		c.Set("claims", claims)

		// Also set in request context for compatibility with GetPrincipal(context.Context)
		ctx := context.WithValue(c.Request.Context(), PrincipalKey, claims.UserID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// validateClaimsStrict performs strict validation of JWT claims
func validateClaimsStrict(claims *JWTClaims, cfg Config) error {
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

// respondWithErrorGin ensures standardized JSON output for Gin-based auth failures
func respondWithErrorGin(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, ErrorResponse{Error: msg})
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// TokenGenerator creates JWT tokens for testing and internal use.
type TokenGenerator struct {
	secret []byte
	issuer string
}

// NewTokenGenerator creates a new token generator.
func NewTokenGenerator(secret string) *TokenGenerator {
	return &TokenGenerator{
		secret: []byte(secret),
		issuer: "stellarbill-backend",
	}
}

// generateToken creates a token with given claims.
func (tg *TokenGenerator) generateToken(userID, email, role string, expiresAt time.Time) (string, error) {
	claims := JWTClaims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tg.issuer,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   userID,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tg.secret)
}

// GenerateAdminToken creates an admin token valid for 24h.
func (tg *TokenGenerator) GenerateAdminToken(userID, email string) (string, error) {
	return tg.generateToken(userID, email, string(RoleAdmin), time.Now().Add(24*time.Hour))
}

// GenerateMerchantToken creates a merchant token.
func (tg *TokenGenerator) GenerateMerchantToken(userID, email, merchantID string) (string, error) {
	_ = merchantID // could embed as custom claim if needed
	return tg.generateToken(userID, email, string(RoleMerchant), time.Now().Add(24*time.Hour))
}

// GenerateCustomerToken creates a customer token.
func (tg *TokenGenerator) GenerateCustomerToken(userID, email string) (string, error) {
	return tg.generateToken(userID, email, string(RoleCustomer), time.Now().Add(24*time.Hour))
}

// GenerateExpiredToken creates a token that is already expired.
func (tg *TokenGenerator) GenerateExpiredToken(userID, email string, role Role) (string, error) {
	return tg.generateToken(userID, email, string(role), time.Now().Add(-1*time.Hour))
}
