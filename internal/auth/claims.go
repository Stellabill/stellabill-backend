package auth

import (
	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims structure
type Claims struct {
	UserID     string   `json:"user_id"`
	Email      string   `json:"email"`
	Role       Role     `json:"role"`
	Roles      []Role   `json:"roles,omitempty"`
	MerchantID string   `json:"merchant_id,omitempty"`
	jwt.RegisteredClaims
}


// AllRoles returns all valid roles
func AllRoles() []Role {
	return []Role{RoleAdmin, RoleMerchant, RoleCustomer, RoleUser}
}

// HasRole checks if claims contain the specified role
func (c *Claims) HasRole(role string) bool {
	if string(c.Role) == role {
		return true
	}
	for _, r := range c.Roles {
		if string(r) == role {
			return true
		}
	}
	return false
}
