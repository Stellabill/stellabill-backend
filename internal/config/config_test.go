package config

import (
	"context"
	"errors"
	"os"
	"stellarbill-backend/internal/secrets"
	"strings"
	"testing"
)

const (
	validDBURL      = "postgres://user:pass@localhost/db"
	validJWTSecret  = "VerySecureJWTSecret123!"
	validAdminToken = "VerySecureAdminToken123!"
)

type stubProvider struct {
	values map[string]string
	errs   map[string]error
}

func (s *stubProvider) GetSecret(_ context.Context, key string) (string, error) {
	if err, ok := s.errs[key]; ok {
		return "", err
	}
	if v, ok := s.values[key]; ok {
		return v, nil
	}
	return "", secrets.ErrSecretNotFound
}

func (s *stubProvider) Name() string {
	return "stub"
}

func withEnvVars(t *testing.T, vars map[string]string, fn func()) {
	t.Helper()
	original := make(map[string]*string, len(vars))
	for k, v := range vars {
		if old, ok := os.LookupEnv(k); ok {
			oldCopy := old
			original[k] = &oldCopy
		} else {
			original[k] = nil
		}
		if v == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, v)
		}
	}
	defer func() {
		for k, old := range original {
			if old == nil {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, *old)
			}
		}
	}()
	fn()
}

func newValidProvider() *stubProvider {
	return &stubProvider{
		values: map[string]string{
			"DATABASE_URL": validDBURL,
			"JWT_SECRET":   validJWTSecret,
			"ADMIN_TOKEN":  validAdminToken,
			"REDIS_URL":    "redis://localhost:6379",
		},
		errs: map[string]error{},
	}
}

func TestLoadValidConfig(t *testing.T) {
	withEnvVars(t, map[string]string{
		"PORT":               "8080",
		"ENV":                "development",
		"RATE_LIMIT_ENABLED": "true",
		"RATE_LIMIT_MODE":    "ip",
		"RATE_LIMIT_RPS":     "10",
		"RATE_LIMIT_BURST":   "20",
	}, func() {
		cfg, err := Load(WithSecretsProvider(newValidProvider()))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if cfg.Port != 8080 {
			t.Fatalf("expected port 8080, got %d", cfg.Port)
		}
		if cfg.JWTSecret != validJWTSecret {
			t.Fatalf("expected JWT secret from provider")
		}
		if cfg.AdminToken != validAdminToken {
			t.Fatalf("expected admin token from provider")
		}
	})
}

func TestLoadMissingRequiredSecrets(t *testing.T) {
	withEnvVars(t, map[string]string{"ENV": "development"}, func() {
		provider := &stubProvider{values: map[string]string{}, errs: map[string]error{}}
		_, err := Load(WithSecretsProvider(provider))
		if err == nil {
			t.Fatal("expected error for missing required secrets")
		}
		msg := err.Error()
		for _, key := range []string{"DATABASE_URL", "JWT_SECRET", "ADMIN_TOKEN"} {
			if !strings.Contains(msg, key) {
				t.Fatalf("expected error to mention %s, got: %s", key, msg)
			}
		}
	})
}

func TestLoadFailsOnWeakSecrets(t *testing.T) {
	withEnvVars(t, map[string]string{"ENV": "development"}, func() {
		provider := &stubProvider{
			values: map[string]string{
				"DATABASE_URL": validDBURL,
				"JWT_SECRET":   "NoSpecial123",
				"ADMIN_TOKEN":  "NoSpecial456",
			},
			errs: map[string]error{},
		}
		_, err := Load(WithSecretsProvider(provider))
		if err == nil {
			t.Fatal("expected weak secret validation error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "WEAK_SECRET") {
			t.Fatalf("expected WEAK_SECRET error, got: %s", msg)
		}
	})
}

func TestLoadRejectsInvalidRateLimitCombination(t *testing.T) {
	withEnvVars(t, map[string]string{
		"ENV":              "development",
		"RATE_LIMIT_MODE":  "invalid",
		"RATE_LIMIT_RPS":   "100",
		"RATE_LIMIT_BURST": "10",
	}, func() {
		_, err := Load(WithSecretsProvider(newValidProvider()))
		if err == nil {
			t.Fatal("expected rate limit validation error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "RATE_LIMIT_MODE") || !strings.Contains(msg, "RATE_LIMIT_BURST") {
			t.Fatalf("expected RATE_LIMIT_MODE and RATE_LIMIT_BURST errors, got: %s", msg)
		}
	})
}

func TestLoadRejectsTimeoutOutOfRange(t *testing.T) {
	withEnvVars(t, map[string]string{
		"ENV":          "development",
		"READ_TIMEOUT": "0",
	}, func() {
		_, err := Load(WithSecretsProvider(newValidProvider()))
		if err == nil {
			t.Fatal("expected invalid timeout error")
		}
		if !strings.Contains(err.Error(), "READ_TIMEOUT") {
			t.Fatalf("expected READ_TIMEOUT in error, got: %v", err)
		}
	})
}

func TestLoadProviderErrorsAreClassified(t *testing.T) {
	withEnvVars(t, map[string]string{"ENV": "development"}, func() {
		provider := &stubProvider{
			values: map[string]string{
				"DATABASE_URL": validDBURL,
			},
			errs: map[string]error{
				"JWT_SECRET":  errors.New("vault unavailable"),
				"ADMIN_TOKEN": secrets.ErrSecretNotFound,
			},
		}
		_, err := Load(WithSecretsProvider(provider))
		if err == nil {
			t.Fatal("expected provider errors")
		}
		msg := err.Error()
		if !strings.Contains(msg, "VALIDATION_FAILED") {
			t.Fatalf("expected VALIDATION_FAILED for provider issue, got: %s", msg)
		}
		if !strings.Contains(msg, "MISSING_ENV_VAR") {
			t.Fatalf("expected MISSING_ENV_VAR for not found secret, got: %s", msg)
		}
	})
}

func TestLoadRejectsShutdownTimeoutOutOfRange(t *testing.T) {
	withEnvVars(t, map[string]string{
		"ENV":                       "development",
		"GRACEFUL_SHUTDOWN_TIMEOUT": "0",
	}, func() {
		_, err := Load(WithSecretsProvider(newValidProvider()))
		if err == nil {
			t.Fatal("expected invalid shutdown timeout error")
		}
		if !strings.Contains(err.Error(), "GRACEFUL_SHUTDOWN_TIMEOUT") {
			t.Fatalf("expected GRACEFUL_SHUTDOWN_TIMEOUT in error, got: %v", err)
		}
	})
}

func TestLoadShutdownTimeoutDefault(t *testing.T) {
	withEnvVars(t, map[string]string{
		"ENV": "development",
	}, func() {
		cfg, err := Load(WithSecretsProvider(newValidProvider()))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if cfg.GracefulShutdownTimeout != DefaultGracefulShutdownTimeout {
			t.Fatalf("expected default shutdown timeout %d, got %d",
				DefaultGracefulShutdownTimeout, cfg.GracefulShutdownTimeout)
		}
	})
}

// TestLoadReplicaURLEnvVars verifies the read-replica DSN is read from the
// canonical DATABASE_REPLICA_URL env var, with DB_REPLICA_URL retained as a
// backward-compatible alias, and that neither is required.
func TestLoadReplicaURLEnvVars(t *testing.T) {
	t.Run("canonical DATABASE_REPLICA_URL wins", func(t *testing.T) {
		withEnvVars(t, map[string]string{
			"ENV":                  "development",
			"DATABASE_REPLICA_URL": "postgres://replica:pass@replica-host:5432/db",
			"DB_REPLICA_URL":       "postgres://legacy:pass@legacy-host:5432/db",
		}, func() {
			cfg, err := Load(WithSecretsProvider(newValidProvider()))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if cfg.DBReplicaConn != "postgres://replica:pass@replica-host:5432/db" {
				t.Fatalf("expected canonical replica URL to win, got %q", cfg.DBReplicaConn)
			}
		})
	})

	t.Run("legacy DB_REPLICA_URL is honoured", func(t *testing.T) {
		withEnvVars(t, map[string]string{
			"ENV":            "development",
			"DB_REPLICA_URL": "postgres://legacy:pass@legacy-host:5432/db",
		}, func() {
			cfg, err := Load(WithSecretsProvider(newValidProvider()))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if cfg.DBReplicaConn != "postgres://legacy:pass@legacy-host:5432/db" {
				t.Fatalf("expected legacy replica URL to be used, got %q", cfg.DBReplicaConn)
			}
		})
	})

	t.Run("absent replica URL is empty", func(t *testing.T) {
		withEnvVars(t, map[string]string{"ENV": "development"}, func() {
			cfg, err := Load(WithSecretsProvider(newValidProvider()))
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if cfg.DBReplicaConn != "" {
				t.Fatalf("expected empty replica URL, got %q", cfg.DBReplicaConn)
			}
		})
	})
}

func TestIsValidSecretRequiresSpecialCharacter(t *testing.T) {
	if isValidSecret("NoSpecialChars123") {
		t.Fatal("expected secret without special char to fail")
	}
	if !isValidSecret(validJWTSecret) {
		t.Fatal("expected strong secret to pass")
	}
}
