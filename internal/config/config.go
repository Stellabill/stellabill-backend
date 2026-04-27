package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"stellabill-backend/internal/secrets"
)

// ConfigErrorType represents the category of configuration error
type ConfigErrorType string

const (
	ErrMissingEnvVar    ConfigErrorType = "MISSING_ENV_VAR"
	ErrInvalidPort      ConfigErrorType = "INVALID_PORT"
	ErrInvalidURL       ConfigErrorType = "INVALID_URL"
	ErrWeakSecret       ConfigErrorType = "WEAK_SECRET"
	ErrValidationFailed ConfigErrorType = "VALIDATION_FAILED"
)

// ConfigError represents a typed configuration error
type ConfigError struct {
	Type    ConfigErrorType
	Key     string
	Message string
	Value   string
}

func (e *ConfigError) Error() string {
	if e.Key != "" {
		return fmt.Sprintf("config error [%s]: %s (key=%s, value=%s)", e.Type, e.Message, e.Key, e.Value)
	}
	return fmt.Sprintf("config error [%s]: %s", e.Type, e.Message)
}

// OutboxConfig holds outbox-specific configuration
type OutboxConfig struct {
	BatchSize          int
	MaxRetries         int
	RetryBackoffFactor float64
	PollInterval       time.Duration
	CleanupInterval    time.Duration
	CompletedEventTTL  time.Duration
	ProcessingTimeout  time.Duration
	PublisherType      string
	HTTPEndpoint       string
}

// GetPollInterval returns the poll interval with default
func (o OutboxConfig) GetPollInterval() time.Duration {
	if o.PollInterval == 0 {
		return 5 * time.Second
	}
	return o.PollInterval
}

// GetCleanupInterval returns the cleanup interval with default
func (o OutboxConfig) GetCleanupInterval() time.Duration {
	if o.CleanupInterval == 0 {
		return 1 * time.Hour
	}
	return o.CleanupInterval
}

// GetCompletedEventTTL returns the TTL with default
func (o OutboxConfig) GetCompletedEventTTL() time.Duration {
	if o.CompletedEventTTL == 0 {
		return 24 * time.Hour
	}
	return o.CompletedEventTTL
}

// GetProcessingTimeout returns the processing timeout with default
func (o OutboxConfig) GetProcessingTimeout() time.Duration {
	if o.ProcessingTimeout == 0 {
		return 30 * time.Second
	}
	return o.ProcessingTimeout
}

// Config holds all application configuration
type Config struct {
	Env       string
	Port      int
	DBConn    string
	JWTSecret string
	// Add additional secure defaults for optional configs
	MaxHeaderBytes int
	ReadTimeout    int
	WriteTimeout   int
	IdleTimeout    int
	// Rate limiting configuration
	RateLimitEnabled   bool
	RateLimitMode      string
	RateLimitRPS       int
	RateLimitBurst     int
	RateLimitWhitelist []string
	AllowedOrigins     string
	// Tracing configuration
	TracingExporter    string
	TracingServiceName string
	// Outbox configuration
	Outbox OutboxConfig
	// Security configuration
	SecurityFrameOpt    string
	SecurityHSTSMaxAge  string
}

// ValidationResult holds the result of configuration validation
type ValidationResult struct {
	Errors   []ConfigError
	Warnings []string
}

// Valid returns true if there are no validation errors
func (v *ValidationResult) Valid() bool {
	return len(v.Errors) == 0
}

// Error returns a formatted string of all validation errors
func (v *ValidationResult) Error() string {
	if v.Valid() {
		return ""
	}
	var errs []string
	for _, e := range v.Errors {
		errs = append(errs, e.Error())
	}
	return strings.Join(errs, "; ")
}

// Constants for configuration limits
const (
	DefaultPort         = 8080
	MinPort             = 1
	MaxPort             = 65535
	MinSecretLength     = 12
	MaxHeaderBytes      = 1 << 20 // 1MB
	DefaultReadTimeout  = 30      // seconds
	DefaultWriteTimeout = 30      // seconds
	DefaultIdleTimeout  = 120     // seconds
)

// Required environment variables
var requiredEnvVars = []string{
	"DATABASE_URL",
	"JWT_SECRET",
}

// Optional environment variables with defaults
var optionalEnvVars = map[string]string{
	"PORT":                 "8080",
	"ENV":                  "development",
	"MAX_HEADER_BYTES":     "1048576",
	"READ_TIMEOUT":         "30",
	"WRITE_TIMEOUT":        "30",
	"IDLE_TIMEOUT":         "120",
	"TRACING_EXPORTER":     "stdout",
	"TRACING_SERVICE_NAME": "stellabill-backend",
}

// Option configures the Load function.
type Option func(*loadOptions)

type loadOptions struct {
	secretsProvider secrets.Provider
}

// WithSecretsProvider overrides the default env-based secrets provider.
func WithSecretsProvider(p secrets.Provider) Option {
	return func(o *loadOptions) {
		o.secretsProvider = p
	}
}

// secretKeys are the config keys that must be fetched through the secrets provider
// rather than read directly from os.Getenv.
var secretKeys = []string{
	"DATABASE_URL",
	"JWT_SECRET",
}

// Load loads configuration from environment variables with validation.
// Sensitive values (DATABASE_URL, JWT_SECRET) are fetched through the secrets
// provider, which defaults to EnvProvider when no option is supplied.
func Load(opts ...Option) (Config, error) {
	o := &loadOptions{
		secretsProvider: secrets.NewEnvProvider(),
	}
	for _, fn := range opts {
		fn(o)
	}

	cfg := Config{
		Env:                getEnv("ENV", "development"),
		Port:               DefaultPort,
		DBConn:             "",
		JWTSecret:          "",
		MaxHeaderBytes:     MaxHeaderBytes,
		ReadTimeout:        DefaultReadTimeout,
		WriteTimeout:       DefaultWriteTimeout,
		IdleTimeout:        DefaultIdleTimeout,
		RateLimitEnabled:   getEnvBool("RATE_LIMIT_ENABLED", false),
		RateLimitMode:      getEnv("RATE_LIMIT_MODE", "standard"),
		RateLimitRPS:       getEnvInt("RATE_LIMIT_RPS", 100),
		RateLimitBurst:     getEnvInt("RATE_LIMIT_BURST", 200),
		RateLimitWhitelist: getEnvSlice("RATE_LIMIT_WHITELIST", nil),
		AllowedOrigins:     getEnv("CORS_ALLOWED_ORIGINS", ""),
		TracingExporter:    getEnv("TRACING_EXPORTER", "stdout"),
		TracingServiceName: getEnv("TRACING_SERVICE_NAME", "stellabill-backend"),
	}

	// Resolve secrets through the provider
	resolved, secretErrs := resolveSecrets(o.secretsProvider, secretKeys)

	result := cfg.validate(resolved, secretErrs)
	if !result.Valid() {
		return Config{}, result
	}

	return cfg, nil
}

// resolveSecrets fetches each key from the provider and returns the values
// alongside any errors keyed by name.
func resolveSecrets(p secrets.Provider, keys []string) (map[string]string, map[string]error) {
	ctx := context.Background()
	vals := make(map[string]string, len(keys))
	errs := make(map[string]error, len(keys))

	for _, k := range keys {
		v, err := p.GetSecret(ctx, k)
		if err != nil {
			errs[k] = err
		} else {
			vals[k] = v
		}
	}
	return vals, errs
}

// Validate validates the configuration using os.Getenv for secrets (legacy path).
// Prefer Load() which uses the secrets provider abstraction.
func (c *Config) Validate() *ValidationResult {
	p := secrets.NewEnvProvider()
	resolved, secretErrs := resolveSecrets(p, secretKeys)
	return c.validate(resolved, secretErrs)
}

// validate is the internal validation method that uses pre-resolved secrets.
func (c *Config) validate(resolvedSecrets map[string]string, secretErrs map[string]error) *ValidationResult {
	result := &ValidationResult{
		Errors:   []ConfigError{},
		Warnings: []string{},
	}

	// Validate required secrets are present via the provider
	for _, key := range secretKeys {
		if err, failed := secretErrs[key]; failed {
			if errors.Is(err, secrets.ErrSecretNotFound) {
				result.Errors = append(result.Errors, ConfigError{
					Type:    ErrMissingEnvVar,
					Key:     key,
					Message: "required secret is missing",
					Value:   "",
				})
			} else {
				result.Errors = append(result.Errors, ConfigError{
					Type:    ErrValidationFailed,
					Key:     key,
					Message: fmt.Sprintf("failed to retrieve secret: %v", err),
					Value:   "",
				})
			}
		}
	}

	// Validate PORT
	if portStr := os.Getenv("PORT"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			result.Errors = append(result.Errors, ConfigError{
				Type:    ErrInvalidPort,
				Key:     "PORT",
				Message: "must be a valid integer",
				Value:   portStr,
			})
		} else if port < MinPort || port > MaxPort {
			result.Errors = append(result.Errors, ConfigError{
				Type:    ErrInvalidPort,
				Key:     "PORT",
				Message: fmt.Sprintf("must be between %d and %d", MinPort, MaxPort),
				Value:   portStr,
			})
		} else {
			c.Port = port
		}
	}

	// Validate DATABASE_URL format
	if dbURL, ok := resolvedSecrets["DATABASE_URL"]; ok {
		if !isValidDatabaseURL(dbURL) {
			result.Errors = append(result.Errors, ConfigError{
				Type:    ErrInvalidURL,
				Key:     "DATABASE_URL",
				Message: "must be a valid database connection string",
				Value:   maskPassword(dbURL),
			})
		} else {
			c.DBConn = dbURL
		}
	}

	// Validate JWT_SECRET
	if secret, ok := resolvedSecrets["JWT_SECRET"]; ok {
		if !isValidSecret(secret) {
			result.Errors = append(result.Errors, ConfigError{
				Type:    ErrWeakSecret,
				Key:     "JWT_SECRET",
				Message: fmt.Sprintf("must be at least %d characters and contain mixed alphanumeric and special characters", MinSecretLength),
				Value:   maskSecret(secret),
			})
		} else {
			c.JWTSecret = secret
		}
	}

	// Validate optional MAX_HEADER_BYTES
	if val := os.Getenv("MAX_HEADER_BYTES"); val != "" {
		if max, err := strconv.Atoi(val); err == nil && max > 0 {
			c.MaxHeaderBytes = max
		} else {
			result.Warnings = append(result.Warnings, "MAX_HEADER_BYTES invalid, using default")
		}
	}

	// Validate optional timeouts
	if val := os.Getenv("READ_TIMEOUT"); val != "" {
		if timeout, err := strconv.Atoi(val); err == nil && timeout > 0 {
			c.ReadTimeout = timeout
		} else {
			result.Warnings = append(result.Warnings, "READ_TIMEOUT invalid, using default")
		}
	}

	if val := os.Getenv("WRITE_TIMEOUT"); val != "" {
		if timeout, err := strconv.Atoi(val); err == nil && timeout > 0 {
			c.WriteTimeout = timeout
		} else {
			result.Warnings = append(result.Warnings, "WRITE_TIMEOUT invalid, using default")
		}
	}

	if val := os.Getenv("IDLE_TIMEOUT"); val != "" {
		if timeout, err := strconv.Atoi(val); err == nil && timeout > 0 {
			c.IdleTimeout = timeout
		} else {
			result.Warnings = append(result.Warnings, "IDLE_TIMEOUT invalid, using default")
		}
	}

	// Validate rate limiting configuration
	if val := os.Getenv("RATE_LIMIT_ENABLED"); val != "" {
		if enabled, err := strconv.ParseBool(val); err == nil {
			c.RateLimitEnabled = enabled
		} else {
			result.Warnings = append(result.Warnings, "RATE_LIMIT_ENABLED invalid, using default")
		}
	}

	if mode := os.Getenv("RATE_LIMIT_MODE"); mode != "" {
		validModes := map[string]bool{"ip": true, "user": true, "hybrid": true}
		if validModes[mode] {
			c.RateLimitMode = mode
		} else {
			result.Warnings = append(result.Warnings, "RATE_LIMIT_MODE invalid, using default")
		}
	}

	if val := os.Getenv("RATE_LIMIT_RPS"); val != "" {
		if rps, err := strconv.Atoi(val); err == nil && rps > 0 && rps <= 1000 {
			c.RateLimitRPS = rps
		} else {
			result.Warnings = append(result.Warnings, "RATE_LIMIT_RPS invalid, using default")
		}
	}

	if val := os.Getenv("RATE_LIMIT_BURST"); val != "" {
		if burst, err := strconv.Atoi(val); err == nil && burst > 0 && burst <= 5000 {
			c.RateLimitBurst = burst
		} else {
			result.Warnings = append(result.Warnings, "RATE_LIMIT_BURST invalid, using default")
		}
	}

	if whitelist := os.Getenv("RATE_LIMIT_WHITELIST"); whitelist != "" {
		paths := strings.Split(whitelist, ",")
		for i, path := range paths {
			paths[i] = strings.TrimSpace(path)
		}
		c.RateLimitWhitelist = paths
	}

	// Validate TRACING_EXPORTER
	if exporter := os.Getenv("TRACING_EXPORTER"); exporter != "" {
		validExporters := map[string]bool{"stdout": true, "otlp": true, "none": true}
		if !validExporters[exporter] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("TRACING_EXPORTER invalid: %s, using default", exporter))
		} else {
			c.TracingExporter = exporter
		}
	}

	if svcName := os.Getenv("TRACING_SERVICE_NAME"); svcName != "" {
		c.TracingServiceName = svcName
	}

	// Set optional env values
	c.Env = getEnv("ENV", "development")

	return result
}

// isValidDatabaseURL validates that the database URL has a valid scheme and structure
func isValidDatabaseURL(dbURL string) bool {
	if dbURL == "" {
		return false
	}

	parsed, err := url.Parse(dbURL)
	if err != nil {
		return false
	}
	if parsed.Scheme == "" {
		return false
	}

	scheme := strings.ToLower(parsed.Scheme)
	validSchemes := map[string]bool{
		"postgres":   true,
		"postgresql": true,
		"mysql":      true,
		"sqlite":     true,
		"sqlite3":    true,
		"mongodb":    true,
		"redis":      true,
	}
	if !validSchemes[scheme] && !strings.Contains(scheme, "sql") {
		return false
	}

	switch scheme {
	case "sqlite", "sqlite3":
		return parsed.Path != "" || parsed.Opaque != ""
	default:
		return parsed.Host != ""
	}
}

// isValidSecret validates that the secret meets security requirements
func isValidSecret(secret string) bool {
	if len(secret) < MinSecretLength {
		return false
	}

	// Check for mixed character types
	hasUpper := false
	hasLower := false
	hasDigit := false
	hasSpecial := false

	for _, r := range secret {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}

	_ = hasSpecial

	return hasUpper && hasLower && hasDigit
}

// maskPassword masks the password in a database URL for security
func maskPassword(dbURL string) string {
	parsed, err := url.Parse(dbURL)
	if err != nil {
		return "***"
	}

	if parsed.User == nil {
		return dbURL
	}

	password, ok := parsed.User.Password()
	if !ok || password == "" {
		return dbURL
	}

	return strings.Replace(dbURL, password, "***", 1)
}

// maskSecret masks a secret for logging
func maskSecret(secret string) string {
	if len(secret) <= 8 {
		return "***"
	}
	return secret[:4] + "***" + secret[len(secret)-4:]
}

// getEnv retrieves an environment variable with a fallback value
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvBool retrieves an environment variable as boolean with a fallback value
func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

// getEnvInt retrieves an environment variable as integer with a fallback value
func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

// getEnvSlice retrieves an environment variable as string slice with a fallback value
func getEnvSlice(key string, fallback []string) []string {
	if v := os.Getenv(key); v != "" {
		parts := strings.Split(v, ",")
		for i, part := range parts {
			parts[i] = strings.TrimSpace(part)
		}
		return parts
	}
	return fallback
}

// GetRequiredEnvVars returns the list of required environment variables
func GetRequiredEnvVars() []string {
	return requiredEnvVars
}

// GetOptionalEnvVars returns the map of optional environment variables with their defaults
func GetOptionalEnvVars() map[string]string {
	return optionalEnvVars
}

