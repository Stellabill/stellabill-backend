package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

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
	Secret       []byte
	Issuer       string
	Audience     string
	Algorithm    string // Required. Must be "HS256" (or "HS384", "HS512").
	ClockSkewSec int64  // Allowed clock drift in seconds. Range: 0-300.
	MaxTokenAge  int64  // Maximum token age beyond expiry in seconds. 0 = disabled.
}

// ValidateConfig checks that the Config meets minimum security requirements.
// It is called once at middleware creation time; a misconfiguration causes an
// immediate panic so the problem surfaces at deploy time rather than at
// request time.
func (cfg *Config) ValidateConfig() {
	if len(cfg.Secret) < 32 {
		panic("auth: JWT secret must be at least 32 bytes")
	}
	if cfg.Issuer == "" {
		panic("auth: JWT issuer is required")
	}
	if cfg.Audience == "" {
		panic("auth: JWT audience is required")
	}
	if cfg.Algorithm == "" {
		panic("auth: JWT algorithm is required")
	}
	if cfg.ClockSkewSec < 0 || cfg.ClockSkewSec > 300 {
		panic("auth: ClockSkewSec must be between 0 and 300")
	}
	if cfg.MaxTokenAge < 0 {
		panic("auth: MaxTokenAge must be non-negative")
	}
}

// validateClaimsStrict enforces issuer, audience, expiry, nbf, and token-age
// checks with bounded clock skew. It returns nil on success or a descriptive
// error suitable for 401 responses.
func validateClaimsStrict(cfg *Config, claims *Claims, now time.Time) error {
	// Issuer — exact match, always required.
	if claims.Issuer != cfg.Issuer {
		return fmt.Errorf("invalid issuer: expected %q, got %q", cfg.Issuer, claims.Issuer)
	}

	// Audience — must contain our required value.
	if !containsString(cfg.Audience, claims.Audience) {
		return fmt.Errorf("invalid audience: required %q not found in %v", cfg.Audience, claims.Audience)
	}

	// ExpiresAt — mandatory; rejected if expired beyond clock skew.
	if claims.ExpiresAt == nil {
		return errors.New("token expiration claim missing")
	}
	skew := time.Duration(cfg.ClockSkewSec) * time.Second
	if now.After(claims.ExpiresAt.Time.Add(skew)) {
		return fmt.Errorf(
			"token expired at %v (now: %v, allowed skew: %ds)",
			claims.ExpiresAt.Time.Format(time.RFC3339),
			now.Format(time.RFC3339),
			cfg.ClockSkewSec,
		)
	}

	// NotBefore — rejected if used before nbf minus clock skew.
	if claims.NotBefore != nil {
		if now.Before(claims.NotBefore.Time.Add(-skew)) {
			return fmt.Errorf(
				"token not valid until %v (now: %v, allowed skew: %ds)",
				claims.NotBefore.Time.Format(time.RFC3339),
				now.Format(time.RFC3339),
				cfg.ClockSkewSec,
			)
		}
	}

	// MaxTokenAge — optional upper bound on token age.
	if cfg.MaxTokenAge > 0 && claims.IssuedAt != nil {
		age := now.Sub(claims.IssuedAt.Time).Seconds()
		if age > float64(cfg.MaxTokenAge) {
			return fmt.Errorf(
				"token too old: issued %ds ago, max age %ds",
				int64(age), cfg.MaxTokenAge,
			)
		}
	}

	return nil
}

// Claims is defined in claims.go

// JWTMiddleware creates a middleware verifying tokens against the provided config
func JWTMiddleware(cfg Config) func(http.Handler) http.Handler {
	// Fail fast: validate config once at startup.
	cfg.ValidateConfig()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				respondWithError(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

const (
	defaultJWKSRefreshTTL = 60 * time.Second
	defaultJWKSNegativeTTL = 60 * time.Second
	defaultHTTPClientTimeout  = 10 * time.Second
)

			tokenString := parts[1]
				if tokenString == "" {
					respondWithError(w, http.StatusUnauthorized, "invalid authorization format")
					return
				}
				claims := &Claims{}

				// Use a large leeway to disable the parser's built-in time
				// validation. validateClaimsStrict applies the configured
				// ClockSkewSec for precise tolerance.
				token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
					// Explicitly validate the signing algorithm to prevent
					// algorithm-confusion attacks (e.g. "none", RSA-to-HMAC
					// substitution).
					if t.Method.Alg() != cfg.Algorithm {
						return nil, fmt.Errorf(
							"unexpected algorithm: expected %s, got %s",
							cfg.Algorithm, t.Method.Alg(),
						)
					}
					return cfg.Secret, nil
				}, jwt.WithLeeway(time.Hour*24*365))

			if err != nil || !token.Valid {
				respondWithError(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}

			// Strict claims validation (issuer, audience, clock skew,
			// not-before, token age).
			if err := validateClaimsStrict(&cfg, claims, time.Now()); err != nil {
				respondWithError(w, http.StatusUnauthorized, err.Error())
				return
			}

			// Attach principal to request context
			ctx := context.WithValue(r.Context(), PrincipalKey, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}

type jwksKeyCache struct {
	mu         sync.RWMutex
	fetchMu    sync.Mutex
	url        string
	client     *http.Client
	refreshTTL  time.Duration
	negativeTTL time.Duration
	keys       map[string]crypto.PublicKey
	fetchedAt  time.Time
	negative   map[string]time.Time
}

// GetPrincipal safely extracts the user ID from the context in downstream handlers
func GetPrincipal(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	val, ok := ctx.Value(PrincipalKey).(string)
	return val, ok
}

// respondWithError ensures standardized JSON output for auth failures
func respondWithError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{Error: msg})
}

func containsString(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// TokenGenerator creates JWT tokens for testing and internal use.
type TokenGenerator struct {
	secret   []byte
	issuer   string
	audience string
}

// NewTokenGenerator creates a new token generator.
func NewTokenGenerator(secret string) *TokenGenerator {
	return &TokenGenerator{
		secret:   []byte(secret),
		issuer:   "stellarbill-backend",
		audience: "api-clients",
	}
}

// generateToken creates a token with given claims.
func (tg *TokenGenerator) generateToken(userID, email, role, tenantID string, expiresAt time.Time) (string, error) {
	claims := Claims{
		UserID:   userID,
		Email:    email,
		Role:     Role(role),
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tg.issuer,
			Audience:  jwt.ClaimStrings{tg.audience},
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
	return tg.generateToken(userID, email, string(RoleAdmin), "tenant-1", time.Now().Add(24*time.Hour))
}

// GenerateMerchantToken creates a merchant token.
func (tg *TokenGenerator) GenerateMerchantToken(userID, email, merchantID string) (string, error) {
	return tg.generateToken(userID, email, string(RoleMerchant), merchantID, time.Now().Add(24*time.Hour))
}

// GenerateCustomerToken creates a customer token.
func (tg *TokenGenerator) GenerateCustomerToken(userID, email string) (string, error) {
	return tg.generateToken(userID, email, string(RoleCustomer), "tenant-1", time.Now().Add(24*time.Hour))
}

// GenerateExpiredToken creates a token that is already expired.
func (tg *TokenGenerator) GenerateExpiredToken(userID, email string, role Role) (string, error) {
	return tg.generateToken(userID, email, string(role), "tenant-1", time.Now().Add(-1*time.Hour))
}

// GenerateTokenWithoutRoles creates a token with no roles assigned.
func (tg *TokenGenerator) GenerateTokenWithoutRoles(userID, email string) (string, error) {
	return tg.generateToken(userID, email, "", "tenant-1", time.Now().Add(24*time.Hour))
}

// GenerateTokenWithoutUserID creates a token missing the user_id/subject claim.
func (tg *TokenGenerator) GenerateTokenWithoutUserID(email, role string) (string, error) {
	claims := Claims{
		Email: email,
		Role:  Role(role),
		TenantID: "tenant-1",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tg.issuer,
			Audience:  jwt.ClaimStrings{tg.audience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tg.secret)
}
