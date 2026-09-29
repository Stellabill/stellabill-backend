package auth

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestCoverage_ExtractRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(RolesContextKey, []Role{RoleAdmin})
	if ExtractRole(c) != RoleAdmin {
		t.Fatal("expected admin role")
	}

	// no roles
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	if ExtractRole(c2) != "" {
		t.Fatal("expected empty")
	}
}

func TestCoverage_RequirePermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mw := RequirePermission(PermReadPlans)

	// authorized
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set(RolesContextKey, []Role{RoleAdmin})
	mw(c)

	// no roles
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest("GET", "/", nil)
	mw(c2)

	// insufficient role
	c3, _ := gin.CreateTestContext(httptest.NewRecorder())
	c3.Request = httptest.NewRequest("GET", "/", nil)
	c3.Set(RolesContextKey, []Role{"unknown_role"})
	mw(c3)
}

func TestCoverage_ExtractRoles_Variants(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// []string
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(RolesContextKey, []string{"admin"})
	_ = ExtractRoles(c)

	// string
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Set(RolesContextKey, "admin")
	_ = ExtractRoles(c2)

	// fallback to RoleContextKey
	c3, _ := gin.CreateTestContext(httptest.NewRecorder())
	c3.Set(RoleContextKey, "merchant")
	_ = ExtractRoles(c3)

	// Role typed
	c4, _ := gin.CreateTestContext(httptest.NewRecorder())
	c4.Set(RoleContextKey, RoleAdmin)
	_ = ExtractRoles(c4)

	// normalizeRoles dedup + skip empty
	c5, _ := gin.CreateTestContext(httptest.NewRecorder())
	c5.Set(RolesContextKey, []string{"admin", "admin", "", "merchant"})
	roles := ExtractRoles(c5)
	if len(roles) != 2 {
		t.Fatalf("expected 2 deduped roles, got %v", roles)
	}
}

func TestCoverage_JWKSCache(t *testing.T) {
	c := &JWKSCache{}
	// empty kid
	c.Set("", "key", time.Second)
	c.Get("")
	// normal set/get
	c.Set("kid", "key", time.Second)
	c.Get("kid")
	// duplicate kid update
	c.Set("kid", "key2", time.Second)
	c.Get("kid")
	// expired entry
	c.Set("expired", "key", -time.Second)
	c.Get("expired")
	// invalid key (nil)
	c.Set("nil", nil, time.Second)
	c.Get("nil")
	// unknown kid
	c.Get("unknown")
}
