package auth

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const RoleContextKey = "role"
const RolesContextKey = "roles"

func ExtractRole(c *gin.Context) Role {
	roles := ExtractRoles(c)
	if len(roles) == 0 {
		return ""
	}
	return roles[0]
}

func ExtractRoles(c *gin.Context) []Role {
	if roles := rolesFromContext(c); len(roles) > 0 {
		return roles
	}

	if role := strings.TrimSpace(c.GetHeader("X-Role")); role != "" {
		return []Role{Role(role)}
	}

	if roles := rolesFromJWT(c); len(roles) > 0 {
		return roles
	}

	return nil
}

func rolesFromContext(c *gin.Context) []Role {
	if value, ok := c.Get(RolesContextKey); ok {
		switch typed := value.(type) {
		case []Role:
			return normalizeRoles(typed)
		case []string:
			roles := make([]Role, 0, len(typed))
			for _, role := range typed {
				roles = append(roles, Role(strings.TrimSpace(role)))
			}
			return normalizeRoles(roles)
		case string:
			return normalizeRoles([]Role{Role(strings.TrimSpace(typed))})
		}
	}

	if value, ok := c.Get(RoleContextKey); ok {
		switch typed := value.(type) {
		case Role:
			return normalizeRoles([]Role{typed})
		case string:
			return normalizeRoles([]Role{Role(strings.TrimSpace(typed))})
		}
	}

	return nil
}

func rolesFromJWT(c *gin.Context) []Role {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader == "" {
		return nil
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret"
	}

	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(parts[1], claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{"HS256", "HS384", "HS512"}))
	if err != nil || !token.Valid {
		return nil
	}

	roles := make([]Role, 0, 2)
	if roleValue, ok := claims["role"]; ok {
		if role, ok := roleValue.(string); ok {
			roles = append(roles, Role(strings.TrimSpace(role)))
		}
	}
	if rolesValue, ok := claims["roles"]; ok {
		switch typed := rolesValue.(type) {
		case []interface{}:
			for _, candidate := range typed {
				if role, ok := candidate.(string); ok {
					roles = append(roles, Role(strings.TrimSpace(role)))
				}
			}
		case []string:
			for _, role := range typed {
				roles = append(roles, Role(strings.TrimSpace(role)))
			}
		case string:
			roles = append(roles, Role(strings.TrimSpace(typed)))
		}
	}

	return normalizeRoles(roles)
}

func normalizeRoles(roles []Role) []Role {
	result := make([]Role, 0, len(roles))
	seen := map[Role]struct{}{}
	for _, role := range roles {
		role = Role(strings.TrimSpace(string(role)))
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		result = append(result, role)
	}
	return result
}

func RequirePermission(permission Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		roles := ExtractRoles(c)
		if len(roles) == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing role",
			})
			return
		}

		for _, role := range roles {
			if HasPermission(role, permission) {
				c.Set(RoleContextKey, role)
				c.Set(RolesContextKey, roles)
				c.Next()
				return
			}
		}

		if len(roles) > 0 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "forbidden",
			})
			return
		}
	}
}
