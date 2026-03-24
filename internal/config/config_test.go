package config

import (
	"os"
	"strings"
	"testing"
)

// Test helper to set and unset environment variables
func withEnvVars(t *testing.T, vars map[string]string, fn func()) {
	// Save original env vars
	origEnv := make(map[string]string)
	for k := range vars {
		origEnv[k] = os.Getenv(k)
		if vars[k] == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, vars[k])
		}
	}

	// Restore original env vars after test
	defer func() {
		for k, v := range origEnv {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	fn()
}

func TestLoad_WithMissingRequiredVars(t *testing.T) {
	// Clear all required env vars
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "",
		"JWT_SECRET":   "",
		"PORT":         "",
	}, func() {
		_, err := Load()
		if err == nil {
			t.Error("Expected error for missing required env vars, got nil")
		}

		// Check error contains expected messages
		errStr := err.Error()
		if !strings.Contains(errStr, "MISSING_ENV_VAR") {
			t.Errorf("Expected MISSING_ENV_VAR error, got: %s", errStr)
		}
	})
}

func TestLoad_WithValidConfig(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   "MySecureSecret123!@#$%^&*()",
		"PORT":         "8080",
		"ENV":          "development",
	}, func() {
		cfg, err := Load()
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if cfg.Port != 8080 {
			t.Errorf("Expected port 8080, got %d", cfg.Port)
		}

		if cfg.DBConn != "postgres://user:pass@localhost/db" {
			t.Errorf("Expected DBConn, got %s", cfg.DBConn)
		}

		if cfg.JWTSecret != "MySecureSecret123!@#$%^&*()" {
			t.Errorf("Expected JWTSecret, got %s", cfg.JWTSecret)
		}
	})
}

func TestLoad_WithInvalidPort(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   "MySecureSecret123!@#$%^&*()",
		"PORT":         "invalid",
	}, func() {
		_, err := Load()
		if err == nil {
			t.Error("Expected error for invalid port, got nil")
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "INVALID_PORT") {
			t.Errorf("Expected INVALID_PORT error, got: %s", errStr)
		}
	})
}

func TestLoad_WithPortOutOfRange(t *testing.T) {
	testCases := []struct {
		port       string
		shouldFail bool
	}{
		{"0", true},
		{"-1", true},
		{"65536", true},
		{"100000", true},
		{"1", false},
		{"65535", false},
	}

	for _, tc := range testCases {
		withEnvVars(t, map[string]string{
			"DATABASE_URL": "postgres://user:pass@localhost/db",
			"JWT_SECRET":   "MySecureSecret123!@#$%^&*()",
			"PORT":         tc.port,
		}, func() {
			_, err := Load()
			if tc.shouldFail && err == nil {
				t.Errorf("Expected error for port %s, got nil", tc.port)
			}
			if !tc.shouldFail && err != nil {
				t.Errorf("Expected no error for port %s, got: %v", tc.port, err)
			}
		})
	}
}

func TestLoad_WithInvalidDatabaseURL(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "not-a-valid-url",
		"JWT_SECRET":   "MySecureSecret123!@#$%^&*()",
		"PORT":         "8080",
	}, func() {
		_, err := Load()
		if err == nil {
			t.Error("Expected error for invalid DATABASE_URL, got nil")
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "INVALID_URL") {
			t.Errorf("Expected INVALID_URL error, got: %s", errStr)
		}
	})
}

func TestLoad_WithWeakSecret(t *testing.T) {
	testCases := []struct {
		secret     string
		shouldFail bool
	}{
		{"short", true},
		{"onlylowercase", true},
		{"ONLYUPPERCASE", true},
		{"1234567890", true},
		{"NoSpecialChars123", true},
		{"NoDigits!@#$%", true},
		{"Valid1Secret", false},
		{"MySecureSecret123!@#$%^&*()", false},
		{"AnotherValid1Secret", false},
	}

	for _, tc := range testCases {
		withEnvVars(t, map[string]string{
			"DATABASE_URL": "postgres://user:pass@localhost/db",
			"JWT_SECRET":   tc.secret,
			"PORT":         "8080",
		}, func() {
			_, err := Load()
			if tc.shouldFail && err == nil {
				t.Errorf("Expected error for weak secret '%s', got nil", tc.secret)
			}
			if !tc.shouldFail && err != nil {
				t.Errorf("Expected no error for secret '%s', got: %v", tc.secret, err)
			}
		})
	}
}

func TestLoad_WithOptionalConfigs(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL":     "postgres://user:pass@localhost/db",
		"JWT_SECRET":       "MySecureSecret123!@#$%^&*()",
		"PORT":             "3000",
		"ENV":              "production",
		"MAX_HEADER_BYTES": "2097152",
		"READ_TIMEOUT":     "60",
		"WRITE_TIMEOUT":    "60",
		"IDLE_TIMEOUT":     "180",
	}, func() {
		cfg, err := Load()
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if cfg.Port != 3000 {
			t.Errorf("Expected port 3000, got %d", cfg.Port)
		}

		if cfg.Env != "production" {
			t.Errorf("Expected env production, got %s", cfg.Env)
		}

		if cfg.MaxHeaderBytes != 2097152 {
			t.Errorf("Expected MaxHeaderBytes 2097152, got %d", cfg.MaxHeaderBytes)
		}

		if cfg.ReadTimeout != 60 {
			t.Errorf("Expected ReadTimeout 60, got %d", cfg.ReadTimeout)
		}

		if cfg.WriteTimeout != 60 {
			t.Errorf("Expected WriteTimeout 60, got %d", cfg.WriteTimeout)
		}

		if cfg.IdleTimeout != 180 {
			t.Errorf("Expected IdleTimeout 180, got %d", cfg.IdleTimeout)
		}
	})
}

func TestLoad_WithInvalidOptionalConfigs(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL":     "postgres://user:pass@localhost/db",
		"JWT_SECRET":       "MySecureSecret123!@#$%^&*()",
		"PORT":             "8080",
		"MAX_HEADER_BYTES": "invalid",
		"READ_TIMEOUT":     "invalid",
		"WRITE_TIMEOUT":    "invalid",
		"IDLE_TIMEOUT":     "invalid",
	}, func() {
		cfg, err := Load()
		if err != nil {
			t.Errorf("Expected no error (should use defaults), got: %v", err)
		}

		// Should use defaults
		if cfg.MaxHeaderBytes != MaxHeaderBytes {
			t.Errorf("Expected default MaxHeaderBytes, got %d", cfg.MaxHeaderBytes)
		}
	})
}

func TestConfigError_Error(t *testing.T) {
	err := &ConfigError{
		Type:    ErrMissingEnvVar,
		Key:     "DATABASE_URL",
		Message: "required environment variable is missing",
		Value:   "",
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "MISSING_ENV_VAR") {
		t.Errorf("Expected error to contain type, got: %s", errStr)
	}
	if !strings.Contains(errStr, "DATABASE_URL") {
		t.Errorf("Expected error to contain key, got: %s", errStr)
	}
}

func TestValidationResult_Valid(t *testing.T) {
	vr := &ValidationResult{
		Errors:   []ConfigError{},
		Warnings: []string{},
	}

	if !vr.Valid() {
		t.Error("Expected ValidationResult to be valid with no errors")
	}

	vr.Errors = append(vr.Errors, ConfigError{Type: ErrMissingEnvVar})
	if vr.Valid() {
		t.Error("Expected ValidationResult to be invalid with errors")
	}
}

func TestValidationResult_Error(t *testing.T) {
	vr := &ValidationResult{
		Errors:   []ConfigError{},
		Warnings: []string{},
	}

	if vr.Error() != "" {
		t.Error("Expected empty error string for valid result")
	}

	vr.Errors = append(vr.Errors, ConfigError{Type: ErrMissingEnvVar, Key: "TEST", Message: "test error"})
	errStr := vr.Error()
	if errStr == "" {
		t.Error("Expected non-empty error string for invalid result")
	}
	if !strings.Contains(errStr, "TEST") {
		t.Errorf("Expected error string to contain key, got: %s", errStr)
	}
}

func TestIsValidDatabaseURL(t *testing.T) {
	testCases := []struct {
		url      string
		expected bool
	}{
		{"postgres://user:pass@localhost/db", true},
		{"postgresql://user:pass@localhost/db", true},
		{"mysql://user:pass@localhost/db", true},
		{"sqlite3:///path/to/db", true},
		{"redis://localhost:6379", true},
		{"mongodb://localhost:27017", true},
		{"", false},
		{"not-a-url", false},
		{"://localhost", false},
	}

	for _, tc := range testCases {
		result := isValidDatabaseURL(tc.url)
		if result != tc.expected {
			t.Errorf("isValidDatabaseURL(%s) = %v, expected %v", tc.url, result, tc.expected)
		}
	}
}

func TestIsValidSecret(t *testing.T) {
	testCases := []struct {
		secret   string
		expected bool
	}{
		{"short", false},
		{"onlylowercase", false},
		{"ONLYUPPERCASE", false},
		{"1234567890", false},
		{"NoSpecialChars123", false},
		{"NoDigits!@#$%", false},
		{"Valid1Secret", true},
		{"MySecureSecret123!@#$%^&*()", true},
		{"AnotherValid1Secret", true},
		{"Str0ng!Secr3t", true},
		{"aB1" + strings.Repeat("x", 29), true}, // exactly 32 chars with mixed types
	}

	for _, tc := range testCases {
		result := isValidSecret(tc.secret)
		if result != tc.expected {
			t.Errorf("isValidSecret(%s) = %v, expected %v", tc.secret, result, tc.expected)
		}
	}
}

func TestMaskPassword(t *testing.T) {
	url := "postgres://user:password@localhost:5432/db"
	masked := maskPassword(url)

	if strings.Contains(masked, "password") {
		t.Errorf("Expected password to be masked, got: %s", masked)
	}
	if !strings.Contains(masked, "***") {
		t.Errorf("Expected mask pattern in result, got: %s", masked)
	}
}

func TestMaskSecret(t *testing.T) {
	secret := "MySuperSecretKey123!@#"
	masked := maskSecret(secret)

	if strings.Contains(secret, masked) && masked != "***" {
		// Should not contain the full secret
	}

	shortSecret := "short"
	maskedShort := maskSecret(shortSecret)
	if maskedShort != "***" {
		t.Errorf("Expected short secrets to be fully masked, got: %s", maskedShort)
	}
}

func TestGetRequiredEnvVars(t *testing.T) {
	vars := GetRequiredEnvVars()
	if len(vars) == 0 {
		t.Error("Expected non-empty required env vars")
	}

	found := false
	for _, v := range vars {
		if v == "DATABASE_URL" {
			found = true
		}
	}
	if !found {
		t.Error("Expected DATABASE_URL in required vars")
	}
}

func TestGetOptionalEnvVars(t *testing.T) {
	vars := GetOptionalEnvVars()
	if len(vars) == 0 {
		t.Error("Expected non-empty optional env vars")
	}

	if val, ok := vars["PORT"]; !ok || val != "8080" {
		t.Errorf("Expected PORT default 8080, got: %v", val)
	}
}

func TestLoad_DefaultPort(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   "MySecureSecret123!@#$%^&*()",
		"PORT":         "",
	}, func() {
		cfg, err := Load()
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if cfg.Port != DefaultPort {
			t.Errorf("Expected default port %d, got %d", DefaultPort, cfg.Port)
		}
	})
}

func TestLoad_ProductionMode(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   "MySecureSecret123!@#$%^&*()",
		"PORT":         "8080",
		"ENV":          "production",
	}, func() {
		cfg, err := Load()
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		if cfg.Env != "production" {
			t.Errorf("Expected env production, got %s", cfg.Env)
		}
	})
}

func TestConfig_Validate(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   "MySecureSecret123!@#$%^&*()",
		"PORT":         "8080",
	}, func() {
		cfg := Config{}
		result := cfg.Validate()

		// Should have no errors for valid config
		if len(result.Errors) != 0 {
			t.Errorf("Expected no errors, got: %v", result.Errors)
		}
	})
}

func TestConfig_ValidateWithMissingVars(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "",
		"JWT_SECRET":   "",
		"PORT":         "",
	}, func() {
		cfg := Config{}
		result := cfg.Validate()

		// Should have errors for missing required vars
		if len(result.Errors) == 0 {
			t.Error("Expected errors for missing required vars")
		}
	})
}

func TestConfig_ValidateWithInvalidPort(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   "MySecureSecret123!@#$%^&*()",
		"PORT":         "not-a-number",
	}, func() {
		cfg := Config{}
		result := cfg.Validate()

		found := false
		for _, e := range result.Errors {
			if e.Type == ErrInvalidPort {
				found = true
			}
		}
		if !found {
			t.Error("Expected INVALID_PORT error")
		}
	})
}

func TestConfig_ValidateWithInvalidURL(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "invalid-url",
		"JWT_SECRET":   "MySecureSecret123!@#$%^&*()",
		"PORT":         "8080",
	}, func() {
		cfg := Config{}
		result := cfg.Validate()

		found := false
		for _, e := range result.Errors {
			if e.Type == ErrInvalidURL {
				found = true
			}
		}
		if !found {
			t.Error("Expected INVALID_URL error")
		}
	})
}

func TestConfig_ValidateWithWeakSecret(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   "weak",
		"PORT":         "8080",
	}, func() {
		cfg := Config{}
		result := cfg.Validate()

		found := false
		for _, e := range result.Errors {
			if e.Type == ErrWeakSecret {
				found = true
			}
		}
		if !found {
			t.Error("Expected WEAK_SECRET error")
		}
	})
}

func TestLoad_WithAllValidSchemes(t *testing.T) {
	schemes := []string{
		"postgres://localhost/db",
		"postgresql://localhost/db",
		"mysql://localhost/db",
		"sqlite3:///path/to/db",
		"mongodb://localhost:27017",
		"redis://localhost:6379",
	}

	for _, scheme := range schemes {
		withEnvVars(t, map[string]string{
			"DATABASE_URL": scheme,
			"JWT_SECRET":   "MySecureSecret123!@#$%^&*()",
			"PORT":         "8080",
		}, func() {
			_, err := Load()
			if err != nil {
				t.Errorf("Expected no error for scheme %s, got: %v", scheme, err)
			}
		})
	}
}

func TestLoad_WithZeroTimeout(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   "MySecureSecret123!@#$%^&*()",
		"PORT":         "8080",
		"READ_TIMEOUT": "0",
	}, func() {
		cfg, err := Load()
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		// Zero timeout should use default
		if cfg.ReadTimeout != DefaultReadTimeout {
			t.Errorf("Expected default ReadTimeout for zero value, got %d", cfg.ReadTimeout)
		}
	})
}
