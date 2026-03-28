package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// EnvProvider reads secrets from environment variables.
type EnvProvider struct {
	// prefix is prepended to every key lookup (e.g. "APP_" turns key "JWT_SECRET" into "APP_JWT_SECRET").
	prefix string
}

// NewEnvProvider returns a provider that reads from os.Getenv.
func NewEnvProvider() *EnvProvider {
	return &EnvProvider{}
}

// NewEnvProviderWithPrefix returns a provider that prepends prefix to every key.
func NewEnvProviderWithPrefix(prefix string) *EnvProvider {
	return &EnvProvider{prefix: prefix}
}

// GetSecret retrieves the value of the environment variable identified by key.
// Returns ErrSecretNotFound if the variable is unset or empty.
// Returns ErrProviderTimeout if the context is already cancelled.
func (p *EnvProvider) GetSecret(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%w: %v", ErrProviderTimeout, err)
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("empty key: %w", ErrSecretNotFound)
	}

	envKey := p.prefix + key
	val := os.Getenv(envKey)
	if val == "" {
		return "", fmt.Errorf("environment variable %q not set: %w", envKey, ErrSecretNotFound)
	}
	return val, nil
}

// Name returns "env".
func (p *EnvProvider) Name() string {
	if p.prefix != "" {
		return "env:" + p.prefix
	}
	return "env"
}
