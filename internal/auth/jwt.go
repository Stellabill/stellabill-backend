package auth

import (
	"context"
	"encoding/json"
	"errors"
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
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" || parts[1] == "" {
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

// TokenGenerator handles JWT generation for testing and authenticated flows
type TokenGenerator struct {
	cfg Config
}

// NewTokenGenerator creates a new TokenGenerator
func NewTokenGenerator(secret string) *TokenGenerator {
	return &TokenGenerator{
		cfg: Config{
			Secret: []byte(secret),
		},
	}
}

func (tg *TokenGenerator) generate(claims *Claims) (string, error) {
	if claims.Subject == "" && claims.UserID != "" {
		claims.Subject = claims.UserID
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tg.cfg.Secret)
}

// GenerateToken creates a JWT for the given user, email and roles
func (tg *TokenGenerator) GenerateToken(userID, email string, roles []string, merchantID string) (string, error) {
	claims := &Claims{
		UserID:     userID,
		Email:      email,
		Role:       roles[0], // backward compatibility
		Roles:      roles,
		MerchantID: merchantID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: tg.cfg.Issuer,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tg.cfg.Secret)
}

// GenerateAdminToken helper for tests
func (tg *TokenGenerator) GenerateAdminToken(userID, email string) (string, error) {
	return tg.GenerateToken(userID, email, []string{string(RoleAdmin)}, "")
}

// GenerateMerchantToken helper for tests
func (tg *TokenGenerator) GenerateMerchantToken(userID, email, merchantID string) (string, error) {
	claims := &Claims{
		UserID:     userID,
		Email:      email,
		Role:       string(RoleMerchant),
		MerchantID: merchantID,
		Tenant:     merchantID, // Use merchantID as tenant
	}
	return tg.generate(claims)
}

func (tg *TokenGenerator) GenerateCustomerToken(userID, email string) (string, error) {
	claims := &Claims{
		UserID: userID,
		Email:  email,
		Role:   string(RoleCustomer),
		Tenant: "customer-tenant", // Default tenant for customers
	}
	return tg.generate(claims)
}

// GenerateExpiredToken helper for tests
func (tg *TokenGenerator) GenerateExpiredToken(userID, email string, role Role) (string, error) {
	claims := &Claims{
		UserID: userID,
		Email:  email,
		Role:   string(role),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tg.cfg.Secret)
}

// GenerateTokenWithoutRoles helper for tests
func (tg *TokenGenerator) GenerateTokenWithoutRoles(userID, email string) (string, error) {
	claims := &Claims{
		UserID: userID,
		Email:  email,
		// No roles
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tg.cfg.Secret)
}

// GenerateTokenWithoutUserID helper for tests
func (tg *TokenGenerator) GenerateTokenWithoutUserID(email string, role Role) (string, error) {
	claims := &Claims{
		Email: email,
		Role:  string(role),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tg.cfg.Secret)
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

