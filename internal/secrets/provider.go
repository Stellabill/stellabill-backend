package secrets

import (
	"context"
	"errors"
)

// ErrSecretNotFound is returned when a secret key does not exist in the provider.
var ErrSecretNotFound = errors.New("secret not found")

// ErrProviderTimeout is returned when a provider fails to respond within the deadline.
var ErrProviderTimeout = errors.New("secret provider timeout")

// Provider is the interface that all secret backends must implement.
type Provider interface {
	// GetSecret retrieves the plaintext value for the given key.
	// Returns ErrSecretNotFound if the key does not exist.
	// Returns ErrProviderTimeout if the context deadline is exceeded.
	GetSecret(ctx context.Context, key string) (string, error)

	// Name returns a human-readable identifier for this provider (e.g. "env", "vault").
	// Must never include secret values.
	Name() string
}

