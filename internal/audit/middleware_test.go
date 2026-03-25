package audit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMiddlewareLogsAuthFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)

	r := gin.New()
	r.Use(Middleware(logger))
	r.GET("/protected", func(c *gin.Context) {
		c.Error(errUnauthorized{})
		c.AbortWithStatus(http.StatusUnauthorized)
	})

	req, _ := http.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer sensitive-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	entries := sink.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected a single audit entry, got %d", len(entries))
	}
	meta := entries[0].Metadata
	if meta["auth_header"] != redactedValue {
		t.Fatalf("authorization header should be redacted, got %#v", meta)
	}
	if entries[0].Action != "auth_failure" || entries[0].Outcome == "" {
		t.Fatalf("unexpected action/outcome: %+v", entries[0])
	}
}

func TestLogActionAddsRequestMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)

	r := gin.New()
	r.Use(Middleware(logger))
	r.GET("/admin/resource", func(c *gin.Context) {
		LogAction(c, "admin_read", "resource-123", "success", nil)
		c.Status(http.StatusOK)
	})

	req, _ := http.NewRequest("GET", "/admin/resource", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	entry := sink.Entries()[0]
	if entry.Metadata["path"] != "/admin/resource" || entry.Metadata["method"] != "GET" {
		t.Fatalf("metadata missing request info: %+v", entry.Metadata)
	}
	if entry.Metadata["client_ip"] == "" {
		t.Fatalf("client ip not recorded")
	}
}

func TestLogActionKeepsMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)

	r := gin.New()
	r.Use(Middleware(logger))
	r.GET("/admin/resource", func(c *gin.Context) {
		LogAction(c, "admin_read", "resource-123", "success", map[string]string{"detail": "kept"})
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/resource", nil)
	r.ServeHTTP(rec, req)

	if sink.Entries()[0].Metadata["detail"] != "kept" {
		t.Fatalf("custom metadata dropped: %+v", sink.Entries()[0].Metadata)
	}
}

func TestResolveActorFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("GET", "/", nil)
	c.Set("actor", "actor-from-context")
	if actor := ResolveActor(c); actor != "actor-from-context" {
		t.Fatalf("expected actor from context, got %s", actor)
	}

	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request, _ = http.NewRequest("GET", "/", nil)
	c2.Request.Header.Set("X-Actor", "header-actor")
	if actor := ResolveActor(c2); actor != "header-actor" {
		t.Fatalf("expected actor from header, got %s", actor)
	}

	c3, _ := gin.CreateTestContext(httptest.NewRecorder())
	c3.Request, _ = http.NewRequest("GET", "/", nil)
	c3.Request.Header.Set("X-User", "user-header")
	if actor := ResolveActor(c3); actor != "user-header" {
		t.Fatalf("expected actor from X-User, got %s", actor)
	}
}

func TestLogActionWithoutLoggerIsNoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/noop", func(c *gin.Context) {
		LogAction(c, "noop", "", "ok", nil)
		c.Status(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/noop", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestLogActionWithInvalidLoggerType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(loggerContextKey, "not-a-logger")
		c.Next()
	})
	r.GET("/invalid", func(c *gin.Context) {
		LogAction(c, "action", "target", "ok", nil)
		c.Status(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/invalid", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMiddlewareWithNilLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware(nil))
	r.GET("/unauthorized", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusUnauthorized)
	})
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/unauthorized", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestResolveActorFallbackToIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("GET", "/", nil)
	c.Request.RemoteAddr = "10.0.0.1:1234"
	if actor := ResolveActor(c); actor != "10.0.0.1" {
		t.Fatalf("expected actor from IP, got %s", actor)
	}
}

func TestLogAuthFailureWithoutErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &MemorySink{}
	logger := NewLogger("secret", sink)
	r := gin.New()
	r.Use(Middleware(logger))
	r.GET("/forbidden", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusForbidden)
	})
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/forbidden", nil)
	r.ServeHTTP(rec, req)
	if len(sink.Entries()) != 1 {
		t.Fatal("expected an entry for forbidden status")
	}
	if sink.Entries()[0].Metadata["reason"] != "" {
		t.Fatal("reason should be empty when no errors are set")
	}
}

type errUnauthorized struct{}

func (errUnauthorized) Error() string { return "missing token" }
