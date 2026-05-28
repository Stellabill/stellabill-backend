package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenGenerator generates JWT tokens for testing and use
type TokenGenerator struct {
	jwtSecret string
}

// NewTokenGenerator creates a new token generator
func NewTokenGenerator(jwtSecret string) *TokenGenerator {
	return &TokenGenerator{jwtSecret: jwtSecret}
}

// GenerateToken creates a signed JWT token
func (tg *TokenGenerator) GenerateToken(userID, email, role, merchantID string, roles []string, expiresAt time.Time) (string, error) {
	claims := &Claims{
		UserID:     userID,
		Email:      email,
		Role:       role,
		Roles:      roles,
		MerchantID: merchantID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(tg.jwtSecret))
}

// GenerateAdminToken creates a token with admin role
func (tg *TokenGenerator) GenerateAdminToken(userID, email string) (string, error) {
	return tg.GenerateToken(userID, email, string(RoleAdmin), "", []string{string(RoleAdmin)}, time.Now().Add(1*time.Hour))
}

// GenerateMerchantToken creates a token with merchant role
func (tg *TokenGenerator) GenerateMerchantToken(userID, email, merchantID string) (string, error) {
	return tg.GenerateToken(userID, email, RoleMerchant, merchantID, []string{RoleMerchant}, time.Now().Add(1*time.Hour))
}

// GenerateCustomerToken creates a token with customer role
func (tg *TokenGenerator) GenerateCustomerToken(userID, email string) (string, error) {
	return tg.GenerateToken(userID, email, RoleCustomer, "", []string{RoleCustomer}, time.Now().Add(1*time.Hour))
}

// GenerateExpiredToken creates an expired token
func (tg *TokenGenerator) GenerateExpiredToken(userID, email, role string) (string, error) {
	return tg.GenerateToken(userID, email, role, "", []string{role}, time.Now().Add(-1*time.Hour))
}

// GenerateTokenWithoutRoles creates a token without roles/role field
func (tg *TokenGenerator) GenerateTokenWithoutRoles(userID, email string) (string, error) {
	claims := &Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(tg.jwtSecret))
}

// GenerateTokenWithoutUserID creates a token without user_id
func (tg *TokenGenerator) GenerateTokenWithoutUserID(email, role string) (string, error) {
	claims := &Claims{
		Email: email,
		Role:  role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(tg.jwtSecret))
}
