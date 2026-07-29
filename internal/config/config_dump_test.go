package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDumpRedactsSecretFields(t *testing.T) {
	cfg := &Config{
		Env:       "production",
		Port:      8443,
		DBConn:    "postgres://user:supersecret@localhost:5432/db",
		JWTSecret: "my-jwt-secret-value-123!",
		AdminToken: "my-admin-token-456!",
	}

	dump := Dump(cfg)

	// Non-secret fields should remain visible
	if dump["env"] != "production" {
		t.Errorf("expected env='production', got %v", dump["env"])
	}
	if dump["port"] != int64(8443) {
		t.Errorf("expected port=8443, got %v", dump["port"])
	}

	// Secret fields must be redacted
	if dump["db_conn"] != redacted {
		t.Errorf("expected db_conn to be redacted, got %v", dump["db_conn"])
	}
	if dump["jwt_secret"] != redacted {
		t.Errorf("expected jwt_secret to be redacted, got %v", dump["jwt_secret"])
	}
	if dump["admin_token"] != redacted {
		t.Errorf("expected admin_token to be redacted, got %v", dump["admin_token"])
	}
}

func TestDumpNoRawSecretsInJSON(t *testing.T) {
	cfg := &Config{
		Env:       "staging",
		DBConn:    "postgres://user:supersecret@localhost/db",
		JWTSecret: "my-jwt-secret-789!",
		AdminToken: "my-admin-token-000!",
		RedisURL:  "redis://:super-redis-pass@localhost:6379",
	}

	dump := Dump(cfg)
	raw, err := json.Marshal(dump)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	body := string(raw)

	// The literal secret values must never appear in the JSON output.
	for _, val := range []string{
		"supersecret",
		"my-jwt-secret-789!",
		"my-admin-token-000!",
		"super-redis-pass",
	} {
		if strings.Contains(body, val) {
			t.Errorf("secret value %q leaked into JSON output", val)
		}
	}

	// The redacted placeholder must appear for each secret field.
	if !strings.Contains(body, `"***REDACTED***"`) {
		t.Error("expected at least one redacted placeholder in JSON output")
	}
}

func TestDumpNonSecretFieldsVisible(t *testing.T) {
	cfg := &Config{
		Env:                    "development",
		Port:                   8080,
		MaxHeaderBytes:         1 << 20,
		ReadTimeout:            30,
		WriteTimeout:           30,
		IdleTimeout:            120,
		RateLimitEnabled:       true,
		RateLimitMode:          "ip",
		RateLimitRPS:           10,
		RateLimitBurst:         20,
		RateLimitWhitelist:     []string{"/health", "/metrics"},
		OTelLogsEnabled:        false,
		DBPoolMaxConns:         25,
		DBPoolMinConns:         2,
		PgBouncerEnabled:       false,
		GracefulShutdownTimeout: 30,
	}

	dump := Dump(cfg)

	tests := []struct {
		key      string
		expected interface{}
	}{
		{"env", "development"},
		{"port", int64(8080)},
		{"max_header_bytes", int64(1 << 20)},
		{"read_timeout", int64(30)},
		{"write_timeout", int64(30)},
		{"idle_timeout", int64(120)},
		{"rate_limit_enabled", true},
		{"rate_limit_mode", "ip"},
		{"rate_limit_rps", int64(10)},
		{"rate_limit_burst", int64(20)},
		{"otel_logs_enabled", false},
		{"db_pool_max_conns", int64(25)},
		{"db_pool_min_conns", int64(2)},
		{"pgbouncer_enabled", false},
		{"graceful_shutdown_timeout", int64(30)},
	}

	for _, tt := range tests {
		got, ok := dump[tt.key]
		if !ok {
			t.Errorf("key %q missing from dump", tt.key)
			continue
		}
		if got != tt.expected {
			t.Errorf("key %q: expected %v, got %v", tt.key, tt.expected, got)
		}
	}
}

func TestDumpRedactsRedisURL(t *testing.T) {
	cfg := &Config{
		RedisURL: "redis://:password@localhost:6379",
	}

	dump := Dump(cfg)

	if dump["redis_url"] != redacted {
		t.Errorf("expected redis_url to be redacted, got %v", dump["redis_url"])
	}
}

func TestDumpRedactsDBReplicaConn(t *testing.T) {
	cfg := &Config{
		DBReplicaConn: "postgres://replica:secret@replica-host:5432/db",
	}

	dump := Dump(cfg)

	if dump["db_replica_conn"] != redacted {
		t.Errorf("expected db_replica_conn to be redacted, got %v", dump["db_replica_conn"])
	}
}

func TestDumpHandlesZeroConfig(t *testing.T) {
	// A zero-value Config should not panic and should produce valid output.
	cfg := &Config{}
	dump := Dump(cfg)

	if dump == nil {
		t.Fatal("expected non-nil dump for zero config")
	}

	// All fields should be present (zero values are fine, just no panic)
	if _, ok := dump["port"]; !ok {
		t.Error("expected port key in dump for zero config")
	}
}

func TestDumpRedactedConstantMatchesSafeValue(t *testing.T) {
	// The redacted constant should match the SafeValue redacted string
	// so that there is a single consistent redaction across the codebase.
	if redacted != "***REDACTED***" {
		t.Errorf("expected redacted constant to be '***REDACTED***', got %q", redacted)
	}
}
