package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ChainProvider tries multiple providers in order and returns the first successful result.
// If all providers fail with ErrSecretNotFound, ChainProvider returns ErrSecretNotFound.
// Any non-ErrSecretNotFound error is returned immediately.
type ChainProvider struct {
	providers []Provider
}

// NewChainProvider creates a provider that tries each provider in the given order.
// At least one provider must be supplied.
func NewChainProvider(providers ...Provider) (*ChainProvider, error) {
	if len(providers) == 0 {
		return nil, errors.New("chain provider requires at least one provider")
	}
	return &ChainProvider{providers: providers}, nil
}

// GetSecret tries each provider in order. Returns the first successful value.
// If a provider returns ErrSecretNotFound, the next provider is tried.
// Any other error is returned immediately, wrapped with the provider name.
func (c *ChainProvider) GetSecret(ctx context.Context, key string) (string, error) {
	var notFoundErrs []string

	for _, p := range c.providers {
		val, err := p.GetSecret(ctx, key)
		if err == nil {
			return val, nil
		}

		if errors.Is(err, ErrSecretNotFound) {
			notFoundErrs = append(notFoundErrs, p.Name())
			continue
		}

		// Non-not-found error — stop immediately
		return "", fmt.Errorf("provider %q: %w", p.Name(), err)
	}

	return "", fmt.Errorf(
		"secret %q not found in providers [%s]: %w",
		key,
		strings.Join(notFoundErrs, ", "),
		ErrSecretNotFound,
	)
}

// Name returns a composite name listing all child providers.
func (c *ChainProvider) Name() string {
	names := make([]string, len(c.providers))
	for i, p := range c.providers {
		names[i] = p.Name()
	}
	return "chain[" + strings.Join(names, "->") + "]"
}

