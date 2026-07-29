package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stellarbill-backend/internal/config"

	"github.com/gin-gonic/gin"
)

func TestConfigDumpHandler_Returns200(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Env:       "production",
		Port:      8443,
		DBConn:    "postgres://user:pass@localhost/db",
		JWTSecret: "my-jwt-secret-123!",
	}

	handler := ConfigDumpHandler(cfg)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/config-dump", nil)

	handler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Non-secret field should be visible
	if resp["env"] != "production" {
		t.Errorf("expected env='production', got %v", resp["env"])
	}

	// Secret fields must be redacted
	if resp["db_conn"] != "***REDACTED***" {
		t.Errorf("expected db_conn to be redacted, got %v", resp["db_conn"])
	}
	if resp["jwt_secret"] != "***REDACTED***" {
		t.Errorf("expected jwt_secret to be redacted, got %v", resp["jwt_secret"])
	}
}

func TestConfigDumpHandler_NoSecretsLeak(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Env:         "staging",
		DBConn:      "postgres://user:supersecret@localhost/db",
		JWTSecret:   "do-not-leak-this-123!",
		AdminToken:  "admin-secret-456!",
		RedisURL:    "redis://:redis-pass@localhost:6379",
	}

	handler := ConfigDumpHandler(cfg)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/config-dump", nil)

	handler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	// The literal secret values must never appear in the response body
	for _, val := range []string{
		"supersecret",
		"do-not-leak-this-123!",
		"admin-secret-456!",
		"redis-pass",
	} {
		if strings.Contains(body, val) {
			t.Errorf("secret value %q leaked into response body", val)
		}
	}

	// The redacted placeholder must appear for each secret field
	if !strings.Contains(body, "***REDACTED***") {
		t.Error("expected redacted placeholder in response body")
	}
}

func TestConfigDumpHandler_ContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{Env: "test"}
	handler := ConfigDumpHandler(cfg)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/config-dump", nil)

	handler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	expected := "application/json; charset=utf-8"
	if got := w.Header().Get("Content-Type"); got != expected {
		t.Errorf("expected Content-Type %q, got %q", expected, got)
	}
}

func TestConfigDumpHandler_NonSecretFieldsPreserved(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Env:                    "development",
		Port:                   8080,
		RateLimitEnabled:       true,
		RateLimitMode:          "ip",
		RateLimitRPS:           10,
		RateLimitBurst:         20,
		RateLimitWhitelist:     []string{"/health", "/metrics"},
		DBPoolMaxConns:         25,
		PgBouncerEnabled:       false,
		GracefulShutdownTimeout: 30,
	}

	handler := ConfigDumpHandler(cfg)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/config-dump", nil)

	handler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	tests := []struct {
		key      string
		expected interface{}
	}{
		{"env", "development"},
		{"port", float64(8080)},
		{"rate_limit_enabled", true},
		{"rate_limit_mode", "ip"},
		{"rate_limit_rps", float64(10)},
		{"rate_limit_burst", float64(20)},
		{"db_pool_max_conns", float64(25)},
		{"pgbouncer_enabled", false},
		{"graceful_shutdown_timeout", float64(30)},
	}

	for _, tt := range tests {
		got, ok := resp[tt.key]
		if !ok {
			t.Errorf("key %q missing from response", tt.key)
			continue
		}
		if got != tt.expected {
			t.Errorf("key %q: expected %v, got %v", tt.key, tt.expected, got)
		}
	}
}

func TestConfigDumpHandler_RateLimitWhitelistAsArray(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Env:                "test",
		RateLimitWhitelist: []string{"/health", "/metrics"},
	}

	handler := ConfigDumpHandler(cfg)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/config-dump", nil)

	handler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	whitelist, ok := resp["rate_limit_whitelist"].([]interface{})
	if !ok {
		t.Fatalf("expected rate_limit_whitelist to be an array, got %T", resp["rate_limit_whitelist"])
	}
	if len(whitelist) != 2 {
		t.Fatalf("expected 2 items, got %d", len(whitelist))
	}
	if whitelist[0] != "/health" {
		t.Errorf("expected first item '/health', got %v", whitelist[0])
	}
	if whitelist[1] != "/metrics" {
		t.Errorf("expected second item '/metrics', got %v", whitelist[1])
	}
}

func TestConfigDumpHandler_ZeroConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// A zero-value config should not panic
	cfg := &config.Config{}
	handler := ConfigDumpHandler(cfg)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/config-dump", nil)

	handler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
