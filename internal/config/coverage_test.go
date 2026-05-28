package config

import (
	"os"
	"testing"
)

func TestCoverage_ConfigError(t *testing.T) {
	e1 := &ConfigError{Type: ErrInvalidURL, Key: "URL", Message: "bad", Value: "***"}
	_ = e1.Error()
	e2 := &ConfigError{Type: ErrInvalidURL, Message: "bad"}
	_ = e2.Error()
}

func TestCoverage_ValidationResult_Valid(t *testing.T) {
	v := &ValidationResult{Errors: nil}
	if !v.Valid() {
		t.Fatal("expected valid")
	}
	if v.Error() != "" {
		t.Fatal("expected empty error for valid result")
	}
	v2 := &ValidationResult{Errors: []ConfigError{{Type: ErrInvalidURL, Message: "bad"}}}
	if v2.Valid() {
		t.Fatal("expected invalid")
	}
	if v2.Error() == "" {
		t.Fatal("expected non-empty error")
	}
}

func TestCoverage_maskPassword(t *testing.T) {
	tests := []string{
		"postgres://user:pass@localhost/db",
		"postgres://user@localhost/db",
		"://malformed",
		"",
	}
	for _, in := range tests {
		_ = maskPassword(in)
	}
}

func TestCoverage_getEnvInt64(t *testing.T) {
	os.Unsetenv("ZZZ_INT64_KEY")
	if v := getEnvInt64("ZZZ_INT64_KEY", 42); v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
	os.Setenv("ZZZ_INT64_KEY", "100")
	defer os.Unsetenv("ZZZ_INT64_KEY")
	if v := getEnvInt64("ZZZ_INT64_KEY", 1); v != 100 {
		t.Fatalf("expected 100, got %d", v)
	}
	os.Setenv("ZZZ_INT64_KEY", "not-a-number")
	if v := getEnvInt64("ZZZ_INT64_KEY", 7); v != 7 {
		t.Fatalf("expected fallback 7, got %d", v)
	}
}

func TestCoverage_getEnvFloat64(t *testing.T) {
	os.Unsetenv("ZZZ_F64_KEY")
	if v := getEnvFloat64("ZZZ_F64_KEY", 1.5); v != 1.5 {
		t.Fatalf("expected 1.5, got %v", v)
	}
	os.Setenv("ZZZ_F64_KEY", "3.14")
	defer os.Unsetenv("ZZZ_F64_KEY")
	if v := getEnvFloat64("ZZZ_F64_KEY", 0); v != 3.14 {
		t.Fatalf("expected 3.14, got %v", v)
	}
	os.Setenv("ZZZ_F64_KEY", "bad")
	if v := getEnvFloat64("ZZZ_F64_KEY", 2.5); v != 2.5 {
		t.Fatalf("expected fallback 2.5, got %v", v)
	}
}

func TestCoverage_maskSecret(t *testing.T) {
	_ = maskSecret("")
	_ = maskSecret("short")
	_ = maskSecret("this-is-a-longer-secret")
}

func TestCoverage_isValidDatabaseURL(t *testing.T) {
	cases := []string{
		"",
		"postgres://user:pass@localhost:5432/db",
		"postgresql://user:pass@localhost/db",
		"mysql://user:pass@localhost/db",
		"://malformed",
		"http://example.com",
		"sqlite:///tmp/db.sqlite",
		"sqlite3:db.sqlite",
		"postgres://", // no host
		"/relative/path", // empty scheme
		"otsql://valid", // contains 'sql' but not in valid list
	}
	for _, c := range cases {
		_ = isValidDatabaseURL(c)
	}
}

func TestCoverage_isValidSecret(t *testing.T) {
	_ = isValidSecret("")
	_ = isValidSecret("short")
	_ = isValidSecret("Mixed1!Secret-123")
	_ = isValidSecret("nospecialchar123ABC")
}

func TestCoverage_Validate_BadPort(t *testing.T) {
	os.Setenv("PORT", "not-a-number")
	defer os.Unsetenv("PORT")
	c := &Config{}
	_ = c.validate(map[string]string{
		"DATABASE_URL": "postgres://u:p@l/d",
		"JWT_SECRET":   "Strong1!Secret-MixedAlphaNumeric@123",
		"ADMIN_TOKEN":  "Strong1!Token-MixedAlphaNumeric@123",
	}, nil)

	os.Setenv("PORT", "70000")
	_ = c.validate(map[string]string{}, nil)

	os.Setenv("MAX_HEADER_BYTES", "bad")
	defer os.Unsetenv("MAX_HEADER_BYTES")
	_ = c.validate(map[string]string{}, nil)
}

func TestCoverage_Validate_BadEnvVars(t *testing.T) {
	envVars := map[string]string{
		"MAX_HEADER_BYTES":     "99999999999",
		"READ_TIMEOUT":         "99999",
		"WRITE_TIMEOUT":        "99999",
		"IDLE_TIMEOUT":         "99999",
		"RATE_LIMIT_ENABLED":   "not-bool",
		"RATE_LIMIT_MODE":      "unknown-mode",
		"RATE_LIMIT_RPS":       "9999999",
		"RATE_LIMIT_BURST":     "9999999",
		"RATE_LIMIT_WHITELIST": "no-slash",
		"TRACING_EXPORTER":     "unknown",
		"TRACING_SERVICE_NAME": "svc",
	}
	for k, v := range envVars {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}
	c := &Config{}
	_ = c.validate(map[string]string{}, nil)
}

func TestCoverage_Validate_BadSecrets(t *testing.T) {
	c := &Config{}
	_ = c.validate(map[string]string{
		"DATABASE_URL": "://malformed",
		"JWT_SECRET":   "weak",
		"ADMIN_TOKEN":  "weak",
	}, nil)
}

func TestCoverage_Validate_GoodEnvVars(t *testing.T) {
	envVars := map[string]string{
		"MAX_HEADER_BYTES":     "65536",
		"READ_TIMEOUT":         "30",
		"WRITE_TIMEOUT":        "30",
		"IDLE_TIMEOUT":         "60",
		"RATE_LIMIT_ENABLED":   "true",
		"RATE_LIMIT_MODE":      "ip",
		"RATE_LIMIT_RPS":       "100",
		"RATE_LIMIT_BURST":     "200",
		"RATE_LIMIT_WHITELIST": "/api/health,/ping",
		"TRACING_EXPORTER":     "stdout",
		"TRACING_SERVICE_NAME": "svc",
	}
	for k, v := range envVars {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}
	c := &Config{}
	_ = c.validate(map[string]string{}, nil)
}

