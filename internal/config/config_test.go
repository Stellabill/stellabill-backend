package config

import (
	"os"
	"strings"
	"testing"
)

const validTestSecret = "MySecureSecret123!@#$%^&*()AbCdEfGh"

func withEnvVars(t *testing.T, vars map[string]string, fn func()) {
	origEnv := make(map[string]string)
	for k := range vars {
		origEnv[k] = os.Getenv(k)
		if vars[k] == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, vars[k])
		}
	}

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
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "",
		"JWT_SECRET":   "",
		"PORT":         "",
	}, func() {
		_, err := Load()
		if err == nil {
			t.Error("Expected error for missing required env vars, got nil")
		}
		if !strings.Contains(err.Error(), "MISSING_ENV_VAR") {
			t.Errorf("Expected MISSING_ENV_VAR error, got: %s", err.Error())
		}
	})
}

func TestLoad_WithValidConfig(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   validTestSecret,
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
		if cfg.JWTSecret != validTestSecret {
			t.Errorf("Expected JWTSecret, got %s", cfg.JWTSecret)
		}
	})
}

func TestLoad_WithInvalidPort(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost/db",
		"JWT_SECRET":   validTestSecret,
		"PORT":         "invalid",
	}, func() {
		_, err := Load()
		if err == nil {
			t.Error("Expected error for invalid port, got nil")
		}
		if !strings.Contains(err.Error(), "INVALID_PORT") {
			t.Errorf("Expected INVALID_PORT error, got: %s", err.Error())
		}
	})
}

func TestLoad_WithInvalidDatabaseURL(t *testing.T) {
	withEnvVars(t, map[string]string{
		"DATABASE_URL": "://localhost",
		"JWT_SECRET":   validTestSecret,
		"PORT":         "8080",
	}, func() {
		_, err := Load()
		if err == nil {
			t.Error("Expected error for invalid DATABASE_URL, got nil")
		}
		if !strings.Contains(err.Error(), "INVALID_URL") {
			t.Errorf("Expected INVALID_URL error, got: %s", err.Error())
		}
	})
}

func TestIsValidSecret(t *testing.T) {
	if !isValidSecret(validTestSecret) {
		t.Fatal("expected validTestSecret to pass")
	}
	if isValidSecret("short") {
		t.Fatal("expected short secret to fail")
	}
}
