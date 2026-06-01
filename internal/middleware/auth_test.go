package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"stellarbill-backend/internal/auth"
)

func TestAuthMiddleware_MissingAuthorizationHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", res.Code)
	}
}

func TestAuthMiddleware_InvalidAuthorizationFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "InvalidFormat token")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", res.Code)
	}
}

func TestAuthMiddleware_TokenValidationFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.here")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", res.Code)
	}
}

func TestAuthMiddleware_InvalidTokenClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token with invalid claims structure
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user123",
	})
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	// Should fail due to missing tenant_id
	if res.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", res.Code)
	}
}

func TestAuthMiddleware_MissingSubjectClaim(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token without subject claim
	claims := jwt.MapClaims{
		"tenant_id": "tenant123",
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", res.Code)
	}
}

func TestAuthMiddleware_MissingTenantID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token without tenant_id claim
	claims := jwt.MapClaims{
		"sub": "user123",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", res.Code)
	}
}

func TestAuthMiddleware_TenantMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token with tenant_id claim
	claims := jwt.MapClaims{
		"sub":       "user123",
		"tenant_id": "tenant123",
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	req.Header.Set("X-Tenant-ID", "different-tenant")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", res.Code)
	}
}

func TestAuthMiddleware_SuccessWithRolesArray(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedRoles []auth.Role
	var capturedCallerID string
	var capturedTenantID string
	
	router.GET("/test", func(c *gin.Context) {
		capturedRoles = auth.ExtractRoles(c)
		capturedCallerID = c.GetString("callerID")
		capturedTenantID = c.GetString("tenantID")
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token with roles array
	claims := jwt.MapClaims{
		"sub":       "user123",
		"tenant_id": "tenant123",
		"roles":     []string{"admin", "merchant"},
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	if capturedCallerID != "user123" {
		t.Errorf("expected callerID user123, got %s", capturedCallerID)
	}

	if capturedTenantID != "tenant123" {
		t.Errorf("expected tenantID tenant123, got %s", capturedTenantID)
	}

	if len(capturedRoles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(capturedRoles))
	}
}

func TestAuthMiddleware_SuccessWithSingleRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedRoles []auth.Role
	var capturedCallerID string
	var capturedTenantID string
	
	router.GET("/test", func(c *gin.Context) {
		capturedRoles = auth.ExtractRoles(c)
		capturedCallerID = c.GetString("callerID")
		capturedTenantID = c.GetString("tenantID")
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token with single role
	claims := jwt.MapClaims{
		"sub":       "user123",
		"tenant_id": "tenant123",
		"role":      "admin",
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	if capturedCallerID != "user123" {
		t.Errorf("expected callerID user123, got %s", capturedCallerID)
	}

	if capturedTenantID != "tenant123" {
		t.Errorf("expected tenantID tenant123, got %s", capturedTenantID)
	}

	if len(capturedRoles) != 1 {
		t.Errorf("expected 1 role, got %d", len(capturedRoles))
	}

	if capturedRoles[0] != auth.RoleAdmin {
		t.Errorf("expected role admin, got %s", capturedRoles[0])
	}
}

func TestAuthMiddleware_SuccessWithEmptyRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedRoles []auth.Role
	var capturedCallerID string
	var capturedTenantID string
	
	router.GET("/test", func(c *gin.Context) {
		capturedRoles = auth.ExtractRoles(c)
		capturedCallerID = c.GetString("callerID")
		capturedTenantID = c.GetString("tenantID")
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token without roles
	claims := jwt.MapClaims{
		"sub":       "user123",
		"tenant_id": "tenant123",
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	if capturedCallerID != "user123" {
		t.Errorf("expected callerID user123, got %s", capturedCallerID)
	}

	if capturedTenantID != "tenant123" {
		t.Errorf("expected tenantID tenant123, got %s", capturedTenantID)
	}

	if len(capturedRoles) != 0 {
		t.Errorf("expected 0 roles, got %d", len(capturedRoles))
	}
}

func TestAuthMiddleware_SuccessWithMultipleRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedRoles []auth.Role
	
	router.GET("/test", func(c *gin.Context) {
		capturedRoles = auth.ExtractRoles(c)
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token with multiple roles
	claims := jwt.MapClaims{
		"sub":       "user123",
		"tenant_id": "tenant123",
		"roles":     []interface{}{"admin", "merchant", "user"},
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	if len(capturedRoles) != 3 {
		t.Errorf("expected 3 roles, got %d", len(capturedRoles))
	}
}

func TestAuthMiddleware_UnknownRoleString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedRoles []auth.Role
	
	router.GET("/test", func(c *gin.Context) {
		capturedRoles = auth.ExtractRoles(c)
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token with unknown role
	claims := jwt.MapClaims{
		"sub":       "user123",
		"tenant_id": "tenant123",
		"role":      "unknown_role",
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	// Unknown roles should still be extracted and stored
	if len(capturedRoles) != 1 {
		t.Errorf("expected 1 role, got %d", len(capturedRoles))
	}

	if capturedRoles[0] != "unknown_role" {
		t.Errorf("expected role unknown_role, got %s", capturedRoles[0])
	}
}

func TestAuthMiddleware_ClaimsProjectionVerification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedRoles []auth.Role
	var capturedCallerID string
	var capturedTenantID string
	
	router.GET("/test", func(c *gin.Context) {
		// Verify all context keys are set
		rolesValue, exists := c.Get(auth.RolesContextKey)
		if !exists {
			t.Error("expected roles to be set in context")
		}
		capturedRoles = rolesValue.([]auth.Role)
		
		capturedCallerID, _ = c.Get("callerID")
		capturedTenantID, _ = c.Get("tenantID")
		
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a comprehensive token
	claims := jwt.MapClaims{
		"sub":       "user123",
		"tenant_id": "tenant123",
		"roles":     []string{"admin", "merchant"},
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	if capturedCallerID != "user123" {
		t.Errorf("expected callerID user123, got %s", capturedCallerID)
	}

	if capturedTenantID != "tenant123" {
		t.Errorf("expected tenantID tenant123, got %s", capturedTenantID)
	}

	if len(capturedRoles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(capturedRoles))
	}
}

func TestAuthMiddleware_TenantIDFromClaimOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedTenantID string
	
	router.GET("/test", func(c *gin.Context) {
		capturedTenantID = c.GetString("tenantID")
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token with tenant_id but no header
	claims := jwt.MapClaims{
		"sub":       "user123",
		"tenant_id": "tenant123",
		"role":      "admin",
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	if capturedTenantID != "tenant123" {
		t.Errorf("expected tenantID tenant123, got %s", capturedTenantID)
	}
}

func TestAuthMiddleware_TenantIDFromHeaderOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedTenantID string
	
	router.GET("/test", func(c *gin.Context) {
		capturedTenantID = c.GetString("tenantID")
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token without tenant_id claim
	claims := jwt.MapClaims{
		"sub":  "user123",
		"role": "admin",
		"exp":  time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	req.Header.Set("X-Tenant-ID", "tenant456")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	if capturedTenantID != "tenant456" {
		t.Errorf("expected tenantID tenant456, got %s", capturedTenantID)
	}
}

func TestAuthMiddleware_RolesDeduplication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedRoles []auth.Role
	
	router.GET("/test", func(c *gin.Context) {
		capturedRoles = auth.ExtractRoles(c)
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token with duplicate roles
	claims := jwt.MapClaims{
		"sub":       "user123",
		"tenant_id": "tenant123",
		"roles":     []string{"admin", "admin", "merchant", "merchant"},
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	// Should deduplicate roles
	if len(capturedRoles) != 2 {
		t.Errorf("expected 2 deduplicated roles, got %d", len(capturedRoles))
	}
}

func TestAuthMiddleware_RoleWhitespaceTrimming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedRoles []auth.Role
	
	router.GET("/test", func(c *gin.Context) {
		capturedRoles = auth.ExtractRoles(c)
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token with roles containing whitespace
	claims := jwt.MapClaims{
		"sub":       "user123",
		"tenant_id": "tenant123",
		"roles":     []string{" admin ", " merchant ", " user "},
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	// Verify whitespace is trimmed
	for _, role := range capturedRoles {
		if len(role) > 0 && (role[0] == ' ' || role[len(role)-1] == ' ') {
			t.Errorf("role %q was not trimmed", role)
		}
	}
}

func TestExtractRolesFromClaims_RolesAsInterfaceArray(t *testing.T) {
	claims := jwt.MapClaims{
		"roles": []interface{}{"admin", "merchant", "user"},
	}

	roles := extractRolesFromClaims(claims)
	if len(roles) != 3 {
		t.Errorf("expected 3 roles, got %d", len(roles))
	}
}

func TestExtractRolesFromClaims_EmptyRolesArray(t *testing.T) {
	claims := jwt.MapClaims{
		"roles": []string{},
	}

	roles := extractRolesFromClaims(claims)
	if len(roles) != 0 {
		t.Errorf("expected 0 roles, got %d", len(roles))
	}
}

func TestExtractRolesFromClaims_RoleFallback(t *testing.T) {
	claims := jwt.MapClaims{
		"role": "admin",
	}

	roles := extractRolesFromClaims(claims)
	if len(roles) != 1 {
		t.Errorf("expected 1 role, got %d", len(roles))
	}

	if roles[0] != auth.RoleAdmin {
		t.Errorf("expected role admin, got %s", roles[0])
	}
}

func TestExtractRolesFromClaims_NoRoles(t *testing.T) {
	claims := jwt.MapClaims{
		"sub": "user123",
	}

	roles := extractRolesFromClaims(claims)
	if len(roles) != 0 {
		t.Errorf("expected 0 roles, got %d", len(roles))
	}
}

func TestInitJWKSCache(t *testing.T) {
	// Test that InitJWKSCache initializes the cache
	InitJWKSCache("https://example.com/jwks", 300)
	if jwksCache == nil {
		t.Error("expected jwksCache to be initialized")
	}
	
	// Reset for other tests
	jwksCache = nil
}

func TestAuthMiddleware_UUIDCallerID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedCallerID string
	
	router.GET("/test", func(c *gin.Context) {
		capturedCallerID = c.GetString("callerID")
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token with UUID as callerID
	callerUUID := uuid.New().String()
	claims := jwt.MapClaims{
		"sub":       callerUUID,
		"tenant_id": "tenant123",
		"role":      "admin",
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	if capturedCallerID != callerUUID {
		t.Errorf("expected callerID %s, got %s", callerUUID, capturedCallerID)
	}
}

func TestAuthMiddleware_TenantClaimFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedTenantID string
	
	router.GET("/test", func(c *gin.Context) {
		capturedTenantID = c.GetString("tenantID")
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Test fallback to "tenant" claim when "tenant_id" is not present
	claims := jwt.MapClaims{
		"sub":    "user123",
		"tenant": "tenant789",
		"role":   "admin",
		"exp":    time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	if capturedTenantID != "tenant789" {
		t.Errorf("expected tenantID tenant789, got %s", capturedTenantID)
	}
}

func TestAuthMiddleware_RolesWithEmptyStrings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	middleware := AuthMiddleware(nil, "")
	router := gin.New()
	router.Use(middleware)
	
	var capturedRoles []auth.Role
	
	router.GET("/test", func(c *gin.Context) {
		capturedRoles = auth.ExtractRoles(c)
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	// Create a token with empty strings in roles array
	claims := jwt.MapClaims{
		"sub":       "user123",
		"tenant_id": "tenant123",
		"roles":     []string{"admin", "", "merchant", ""},
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", res.Code)
	}

	// Empty strings should be filtered out
	if len(capturedRoles) != 2 {
		t.Errorf("expected 2 non-empty roles, got %d", len(capturedRoles))
	}
}