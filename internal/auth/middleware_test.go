package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupRouter(permission Permission) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.GET("/test", RequirePermission(permission), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	return r
}

func TestRequirePermission_AdminAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.GET("/test", func(c *gin.Context) {
		c.Set(RoleContextKey, string(RoleAdmin))
		c.Next()
	}, RequirePermission(PermManagePlans), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req, _ := http.NewRequest("GET", "/test", nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequirePermission_UserDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.GET("/test", func(c *gin.Context) {
		c.Set(RoleContextKey, string(RoleUser))
		c.Next()
	}, RequirePermission(PermManagePlans), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req, _ := http.NewRequest("GET", "/test", nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestRequirePermission_MissingRole(t *testing.T) {
	r := setupRouter(PermReadPlans)

	req, _ := http.NewRequest("GET", "/test", nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHasPermission_DefaultDeny(t *testing.T) {
	if HasPermission(Role("unknown"), PermReadPlans) {
		t.Fatal("expected false for unknown role")
	}
}

func TestRequirePermission_MultipleRolesAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		c.Set(RolesContextKey, []string{"customer", "merchant"})
		c.Next()
	}, RequirePermission(PermReadSubscriptions), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
