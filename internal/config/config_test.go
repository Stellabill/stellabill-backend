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

func TestIsValidSecretRequiresSpecialCharacter(t *testing.T) {
	if isValidSecret("NoSpecialChars123") {
		t.Fatal("expected secret without special char to fail")
	}
	if !isValidSecret(validJWTSecret) {
		t.Fatal("expected strong secret to pass")
	}
}
// TestEnvExampleValuesPassValidation verifies that every value shown in
// .env.example produces a valid config when fed through Load().
// This prevents the example file from going stale relative to config.go.
func TestEnvExampleValuesPassValidation(t *testing.T) {
	withEnvVars(t, map[string]string{
		// Application
		"ENV":  "development",
		"PORT": "8080",
		// HTTP tuning
		"MAX_HEADER_BYTES": "1048576",
		"READ_TIMEOUT":     "30",
		"WRITE_TIMEOUT":    "30",
		"IDLE_TIMEOUT":     "120",
		// Request / body size limits
		"MAX_REQUEST_SIZE":     "10485760",
		"MAX_GZIP_UNCOMPRESSED": "52428800",
		"MAX_GZIP_RATIO":       "10.0",
		// Security headers
		"SECURITY_FRAME_ANCESTORS": "'none'",
		// Rate limiting
		"RATE_LIMIT_ENABLED":   "true",
		"RATE_LIMIT_MODE":      "ip",
		"RATE_LIMIT_RPS":       "10",
		"RATE_LIMIT_BURST":     "20",
		"RATE_LIMIT_WHITELIST": "/api/health",
		// Tracing
		"TRACING_EXPORTER":     "stdout",
		"TRACING_SERVICE_NAME": "stellabill-backend",
		// DB pool
		"DB_POOL_MAX_CONNS":           "25",
		"DB_POOL_MIN_CONNS":           "2",
		"DB_POOL_MAX_CONN_LIFETIME":   "3600",
		"DB_POOL_MAX_CONN_IDLE_TIME":  "600",
		"DB_POOL_CONNECT_TIMEOUT":     "5",
		"DB_POOL_HEALTH_CHECK_PERIOD": "30",
		"DB_POOL_METRICS_INTERVAL":    "15",
	}, func() {
		provider := &stubProvider{
			values: map[string]string{
				"DATABASE_URL": "postgres://stellabill:changeme@localhost:5432/stellabill_dev?sslmode=disable",
				"JWT_SECRET":   "CHANGE_ME_jwt_Secret1!",
				"ADMIN_TOKEN":  "CHANGE_ME_admin_Token1!",
			},
			errs: map[string]error{},
		}
		cfg, err := Load(WithSecretsProvider(provider))
		if err != nil {
			t.Fatalf(".env.example values failed config.Load(): %v", err)
		}
		// Spot-check a representative sample of parsed fields.
		if cfg.Port != 8080 {
			t.Errorf("expected Port=8080, got %d", cfg.Port)
		}
		if cfg.ReadTimeout != 30 {
			t.Errorf("expected ReadTimeout=30, got %d", cfg.ReadTimeout)
		}
		if cfg.RateLimitRPS != 10 {
			t.Errorf("expected RateLimitRPS=10, got %d", cfg.RateLimitRPS)
		}
		if cfg.RateLimitBurst != 20 {
			t.Errorf("expected RateLimitBurst=20, got %d", cfg.RateLimitBurst)
		}
		if cfg.DBPoolMaxConns != 25 {
			t.Errorf("expected DBPoolMaxConns=25, got %d", cfg.DBPoolMaxConns)
		}
		if cfg.DBPoolMinConns != 2 {
			t.Errorf("expected DBPoolMinConns=2, got %d", cfg.DBPoolMinConns)
		}
		if cfg.TracingExporter != "stdout" {
			t.Errorf("expected TracingExporter=stdout, got %s", cfg.TracingExporter)
		}
		if cfg.MaxRequestSize != 10485760 {
			t.Errorf("expected MaxRequestSize=10485760, got %d", cfg.MaxRequestSize)
		}
	})
}

func TestLoadReplicaConfig(t *testing.T) {
	t.Run("replica url configured and valid", func(t *testing.T) {
		provider := &stubProvider{
			values: map[string]string{
				"DATABASE_URL":          validDBURL,
				"DATABASE_REPLICA_URL":  "postgres://replica-user:replica-pass@localhost:5432/replica_db",
				"JWT_SECRET":            validJWTSecret,
				"ADMIN_TOKEN":           validAdminToken,
			},
		}
		cfg, err := Load(WithSecretsProvider(provider))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if cfg.DBReplicaConn != "postgres://replica-user:replica-pass@localhost:5432/replica_db" {
			t.Fatalf("expected replica DSN, got %s", cfg.DBReplicaConn)
		}
	})

	t.Run("replica url missing fallback to primary", func(t *testing.T) {
		provider := &stubProvider{
			values: map[string]string{
				"DATABASE_URL":          validDBURL,
				"JWT_SECRET":            validJWTSecret,
				"ADMIN_TOKEN":           validAdminToken,
			},
		}
		cfg, err := Load(WithSecretsProvider(provider))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if cfg.DBReplicaConn != validDBURL {
			t.Fatalf("expected fallback to primary DSN, got %s", cfg.DBReplicaConn)
		}
	})

	t.Run("replica url invalid format", func(t *testing.T) {
		provider := &stubProvider{
			values: map[string]string{
				"DATABASE_URL":          validDBURL,
				"DATABASE_REPLICA_URL":  "://invalid-dsn",
				"JWT_SECRET":            validJWTSecret,
				"ADMIN_TOKEN":           validAdminToken,
			},
		}
		_, err := Load(WithSecretsProvider(provider))
		if err == nil {
			t.Fatal("expected error for invalid replica url")
		}
		if !strings.Contains(err.Error(), "DATABASE_REPLICA_URL") {
			t.Fatalf("expected error message to mention DATABASE_REPLICA_URL, got %v", err)
		}
	})
}

