package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestDeprecatedHandler_EmitsStructuredHeadersFromEnv(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sunset := time.Date(2026, time.December, 31, 23, 59, 59, 0, time.UTC)
	t.Setenv(LegacyAPISunsetEnv, `"`+sunset.Format(time.RFC3339)+`"`)

	r := gin.New()
	r.Use(DeprecatedHandler())
	r.GET("/api/subscriptions/:id", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions/sub-123", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("expected Deprecation=true, got %q", got)
	}
	if got, want := rec.Header().Get("Sunset"), sunset.Format(http.TimeFormat); got != want {
		t.Fatalf("expected Sunset %q, got %q", want, got)
	}
	if got, want := rec.Header().Get("Link"), `</api/v1/subscriptions/sub-123>; rel="successor-version"`; got != want {
		t.Fatalf("expected Link %q, got %q", want, got)
	}
}

func TestDeprecatedHandler_DoesNotMarkV1Routes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(LegacyAPISunsetEnv, "Thu, 31 Dec 2026 23:59:59 GMT")

	r := gin.New()
	r.Use(DeprecatedHandler())
	r.GET("/api/v1/subscriptions/:id", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions/sub-123", nil)
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Deprecation"); got != "" {
		t.Fatalf("expected no Deprecation header, got %q", got)
	}
	if got := rec.Header().Get("Sunset"); got != "" {
		t.Fatalf("expected no Sunset header, got %q", got)
	}
	if got := rec.Header().Get("Link"); got != "" {
		t.Fatalf("expected no Link header, got %q", got)
	}
}

func TestDeprecatedHandler_PreservesHeadersOn4xx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(LegacyAPISunsetEnv, "Thu, 31 Dec 2026 23:59:59 GMT")

	r := gin.New()
	r.Use(DeprecatedHandler())
	r.GET("/api/plans", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusForbidden)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if got := rec.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("expected Deprecation=true on 4xx, got %q", got)
	}
	if got := rec.Header().Get("Sunset"); got == "" {
		t.Fatal("expected Sunset header on 4xx")
	}
	if got := rec.Header().Get("Link"); got == "" {
		t.Fatal("expected Link header on 4xx")
	}
}

func TestDeprecatedHandler_PreservesHeadersAfterPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(LegacyAPISunsetEnv, "Thu, 31 Dec 2026 23:59:59 GMT")

	r := gin.New()
	r.Use(Recovery())
	r.GET("/api/subscriptions", DeprecatedHandler(), func(c *gin.Context) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if got := rec.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("expected Deprecation=true after panic, got %q", got)
	}
	if got := rec.Header().Get("Sunset"); got == "" {
		t.Fatal("expected Sunset header after panic")
	}
	if got := rec.Header().Get("Link"); got == "" {
		t.Fatal("expected Link header after panic")
	}
}

func TestDeprecatedHandler_OmitsInvalidSunset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(LegacyAPISunsetEnv, "not a valid HTTP date")

	r := gin.New()
	r.Use(DeprecatedHandler())
	r.GET("/api/plans", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("expected Deprecation=true, got %q", got)
	}
	if got := rec.Header().Get("Sunset"); got != "" {
		t.Fatalf("expected invalid Sunset env to be omitted, got %q", got)
	}
	if got, want := rec.Header().Get("Link"), `</api/v1/plans>; rel="successor-version"`; got != want {
		t.Fatalf("expected Link %q, got %q", want, got)
	}
}
