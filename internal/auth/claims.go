package auth

import (
	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims structure
// Security notes:
// - UserID is extracted from either Subject or custom user_id claim
// - Role and Roles support both single-role (legacy) and multi-role (RBAC) patterns
// - MerchantID provides tenant isolation
// - All claims are validated by middleware before reaching handlers
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
// Supports both single-role (Role field) and multi-role (Roles slice) patterns
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
