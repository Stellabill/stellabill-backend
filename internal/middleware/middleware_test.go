package middleware

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestProtectedChainContextPropagationAndLogging(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)
	limiter := NewRateLimiter(5, time.Minute)

	router := gin.New()
	router.Use(
		Recovery(logger),
		RequestID(),
		Logging(logger),
		CORS("https://frontend.example"),
		RateLimit(limiter),
		Auth("top-secret"),
	)
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"request_id": c.MustGet(RequestIDKey),
			"subject":    c.MustGet(AuthSubjectKey),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer top-secret")
	req.Header.Set(RequestIDHeader, "req-123")
	req.Header.Set("X-Tenant-ID", "tenant-42")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if got := res.Header().Get(RequestIDHeader); got != "req-123" {
		t.Fatalf("expected response request id header, got %q", got)
	}

	var body map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["request_id"] != "req-123" {
		t.Fatalf("expected request id in body, got %q", body["request_id"])
	}
	if body["subject"] != "api-client" {
		t.Fatalf("expected auth subject in body, got %q", body["subject"])
	}

	var logEntry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &logEntry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}
	if logEntry["request_id"] != "req-123" || int(logEntry["status"].(float64)) != http.StatusOK {
		t.Fatalf("unexpected structured log entry: %#v", logEntry)
	}
	if logEntry["actor"] != "api-client" || logEntry["tenant"] != "tenant-42" {
		t.Fatalf("expected canonical actor and tenant fields, got %#v", logEntry)
	}
}

func TestMiddlewareOrderHarness(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var order []string
	router := gin.New()
	router.Use(
		record("recovery", &order),
		record("request-id", &order),
		record("logging", &order),
		record("cors", &order),
		record("rate-limit", &order),
		record("auth", &order),
	)
	router.GET("/matrix", func(c *gin.Context) {
		order = append(order, "handler")
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/matrix", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	expected := []string{
		"recovery:before",
		"request-id:before",
		"logging:before",
		"cors:before",
		"rate-limit:before",
		"auth:before",
		"handler",
		"auth:after",
		"rate-limit:after",
		"cors:after",
		"logging:after",
		"request-id:after",
		"recovery:after",
	}

	if len(order) != len(expected) {
		t.Fatalf("unexpected order length: got %v want %v", order, expected)
	}
	for i := range expected {
		if order[i] != expected[i] {
			t.Fatalf("unexpected order at %d: got %q want %q full=%v", i, order[i], expected[i], order)
		}
	}
}

func TestPreflightShortCircuitsBeforeRateLimitAndAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var order []string
	router := gin.New()
	router.Use(
		RequestID(),
		CORS("*"),
		record("after-cors", &order),
		RateLimit(NewRateLimiter(1, time.Minute)),
		Auth("secret"),
	)
	router.OPTIONS("/protected", func(c *gin.Context) {
		order = append(order, "handler")
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodOptions, "/protected", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}
	if got := len(order); got != 0 {
		t.Fatalf("expected preflight to stop chain before downstream middleware, got %v", order)
	}
	if res.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected CORS header on preflight response")
	}
}

func TestAuthFailureShortCircuitsWithRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestID(), CORS("*"), RateLimit(NewRateLimiter(2, time.Minute)), Auth("secret"))
	router.GET("/protected", func(c *gin.Context) {
		t.Fatal("handler should not run on unauthorized request")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set(RequestIDHeader, "req-auth-fail")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
	if got := res.Header().Get(RequestIDHeader); got != "req-auth-fail" {
		t.Fatalf("expected request id header, got %q", got)
	}
	assertBodyField(t, res, "error", "unauthorized")
	assertBodyField(t, res, "request_id", "req-auth-fail")
}

func TestRateLimitShortCircuits(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestID(), RateLimit(NewRateLimiter(1, time.Minute)))
	router.GET("/limited", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	firstReq := httptest.NewRequest(http.MethodGet, "/limited", nil)
	firstRes := httptest.NewRecorder()
	router.ServeHTTP(firstRes, firstReq)
	if firstRes.Code != http.StatusNoContent {
		t.Fatalf("expected first request to succeed, got %d", firstRes.Code)
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/limited", nil)
	secondReq.Header.Set(RequestIDHeader, "req-ratelimit")
	secondRes := httptest.NewRecorder()
	router.ServeHTTP(secondRes, secondReq)

	if secondRes.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to be rate limited, got %d", secondRes.Code)
	}
	assertBodyField(t, secondRes, "error", "rate limit exceeded")
	assertBodyField(t, secondRes, "request_id", "req-ratelimit")
}

func TestRecoveryReturnsStructuredError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var logs bytes.Buffer
	logger := log.New(&logs, "", 0)

	router := gin.New()
	router.Use(Recovery(logger), RequestID(), Auth("secret"))
	router.GET("/panic", func(c *gin.Context) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set(RequestIDHeader, "req-panic")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", res.Code)
	}
	assertBodyField(t, res, "error", "internal server error")
	assertBodyField(t, res, "request_id", "req-panic")
	var logEntry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &logEntry); err != nil {
		t.Fatalf("decode panic log: %v", err)
	}
	if logEntry["message"] != "panic recovered" {
		t.Fatalf("unexpected panic log message: %#v", logEntry)
	}
	if logEntry["request_id"] != "req-panic" {
		t.Fatalf("expected request id in panic log, got %#v", logEntry)
	}
}


func TestRateLimiterWindowReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)
	limiter := NewRateLimiter(1, time.Minute)
	limiter.now = func() time.Time { return now }

	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected first request to pass")
	}
	if limiter.Allow("127.0.0.1") {
		t.Fatal("expected second request in same window to fail")
	}

	now = now.Add(2 * time.Minute)
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected request after window reset to pass")
	}
}

func record(name string, order *[]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		*order = append(*order, name+":before")
		c.Next()
		*order = append(*order, name+":after")
	}
}

func assertBodyField(t *testing.T, res *httptest.ResponseRecorder, key, want string) {
	t.Helper()

	var body map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got := body[key]; got != want {
		t.Fatalf("expected %s=%q, got %q", key, want, got)
	}
}

func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

func TestDeprecationHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/api/plans", DeprecationHeaders(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.GET("/api/subscriptions/:id", DeprecationHeaders(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	t.Run("sets Deprecation and Sunset headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", res.Code)
		}
		if got := res.Header().Get("Deprecation"); got != "true" {
			t.Fatalf("expected Deprecation=true, got %q", got)
		}
		if got := res.Header().Get("Sunset"); got == "" {
			t.Fatal("expected Sunset header to be set")
		}
	})

	t.Run("Link header points to v1 equivalent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		want := `</api/v1/plans>; rel="successor-version"`
		if got := res.Header().Get("Link"); got != want {
			t.Fatalf("expected Link=%q, got %q", want, got)
		}
	})

	t.Run("Link header includes path params", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/subscriptions/sub_42", nil)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		want := `</api/v1/subscriptions/sub_42>; rel="successor-version"`
		if got := res.Header().Get("Link"); got != want {
			t.Fatalf("expected Link=%q, got %q", want, got)
		}
	})

	t.Run("handler still executes after deprecation middleware", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Fatalf("expected handler to run and return 200, got %d", res.Code)
		}
	})
}

func TestDeprecationHeadersNotOnV1(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	// v1 route without deprecation middleware — simulates correct wiring
	router.GET("/api/v1/plans", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plans", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if got := res.Header().Get("Deprecation"); got != "" {
		t.Fatalf("v1 routes should not have Deprecation header, got %q", got)
	}
	if got := res.Header().Get("Sunset"); got != "" {
		t.Fatalf("v1 routes should not have Sunset header, got %q", got)
	}
}
