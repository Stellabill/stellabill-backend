package auth

import (
	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims structure
type Claims struct {
	UserID     string   `json:"user_id"`
	Email      string   `json:"email"`
	Role       string   `json:"role"`
	Roles      []string `json:"roles,omitempty"`
	MerchantID string   `json:"merchant_id,omitempty"`
	jwt.RegisteredClaims
}

// Role-based access control roles
const (
	RoleAdmin    = "admin"
	RoleMerchant = "merchant"
	RoleCustomer = "customer"
)

// AllRoles returns all valid roles
func AllRoles() []string {
	return []string{RoleAdmin, RoleMerchant, RoleCustomer}
}

// HasRole checks if claims contain the specified role
func (c *Claims) HasRole(role string) bool {
	if c.Role == role {
		return true
	}
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}
