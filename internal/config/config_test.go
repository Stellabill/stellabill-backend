package config

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"stellarbill-backend/internal/secrets"
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

func TestLoadProductionRequiresAllowedOrigins(t *testing.T) {
	withEnvVars(t, map[string]string{
		"ENV":             "production",
		"ALLOWED_ORIGINS": "",
	}, func() {
		_, err := Load(WithSecretsProvider(newValidProvider()))
		if err == nil {
			t.Fatal("expected missing ALLOWED_ORIGINS error")
		}
		if !strings.Contains(err.Error(), "ALLOWED_ORIGINS") {
			t.Fatalf("expected ALLOWED_ORIGINS in error, got: %v", err)
		}
	})
}

func TestLoadProductionRejectsInsecureAllowedOrigins(t *testing.T) {
	withEnvVars(t, map[string]string{
		"ENV":             "production",
		"ALLOWED_ORIGINS": "http://example.com,https://ok.example.com",
	}, func() {
		_, err := Load(WithSecretsProvider(newValidProvider()))
		if err == nil {
			t.Fatal("expected invalid ALLOWED_ORIGINS error")
		}
		if !strings.Contains(err.Error(), "INVALID_URL") {
			t.Fatalf("expected INVALID_URL in error, got: %v", err)
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

func TestLoadAccumulatesMultipleErrors(t *testing.T) {
	withEnvVars(t, map[string]string{
		"ENV":              "production",
		"PORT":             "70000",
		"ALLOWED_ORIGINS":  "http://insecure.example.com",
		"RATE_LIMIT_BURST": "0",
	}, func() {
		provider := &stubProvider{
			values: map[string]string{
				"DATABASE_URL": "://bad",
				"JWT_SECRET":   "weak",
				"ADMIN_TOKEN":  "weak",
			},
			errs: map[string]error{},
		}
		_, err := Load(WithSecretsProvider(provider))
		if err == nil {
			t.Fatal("expected validation errors")
		}
		msg := err.Error()
		checks := []string{"INVALID_PORT", "INVALID_URL", "WEAK_SECRET", "RATE_LIMIT_BURST", "ALLOWED_ORIGINS"}
		for _, c := range checks {
			if !strings.Contains(msg, c) {
				t.Fatalf("expected error to include %s, got: %s", c, msg)
			}
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

func TestIsValidSecretRequiresSpecialCharacter(t *testing.T) {
	if isValidSecret("NoSpecialChars123") {
		t.Fatal("expected secret without special char to fail")
	}
	if !isValidSecret(validJWTSecret) {
		t.Fatal("expected strong secret to pass")
	}
}