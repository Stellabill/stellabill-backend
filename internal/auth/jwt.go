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
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   Role(role),
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

// GenerateTokenWithoutRoles creates a token with no roles assigned.
func (tg *TokenGenerator) GenerateTokenWithoutRoles(userID, email string) (string, error) {
	return tg.generateToken(userID, email, "", time.Now().Add(24*time.Hour))
}

// GenerateTokenWithoutUserID creates a token missing the user_id/subject claim.
func (tg *TokenGenerator) GenerateTokenWithoutUserID(email, role string) (string, error) {
	claims := Claims{
		Email: email,
		Role:  Role(role),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tg.issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(tg.secret)
}

