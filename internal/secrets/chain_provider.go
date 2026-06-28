package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

func NewDefaultProvider() Provider {
	env := NewEnvProvider()
	addr := os.Getenv("VAULT_ADDR")
	if addr == "" {
		return env
	}

	vault := NewVaultProvider(
		addr,
		os.Getenv("VAULT_TOKEN"),
		os.Getenv("VAULT_PATH_PREFIX"),
	)

	chain, err := NewChainProvider(vault, env)
	if err != nil {
		return env
	}
	return chain
}

type ChainProvider struct {
	providers []Provider
}

func NewChainProvider(providers ...Provider) (*ChainProvider, error) {
	if len(providers) == 0 {
		return nil, errors.New("chain provider requires at least one provider")
	}
	return &ChainProvider{providers: providers}, nil
}

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
		return "", fmt.Errorf("provider %q: %w", p.Name(), err)
	}

	return "", fmt.Errorf(
		"secret %q not found in providers [%s]: %w",
		key,
		strings.Join(notFoundErrs, ", "),
		ErrSecretNotFound,
	)
}

func (c *ChainProvider) Name() string {
	names := make([]string, len(c.providers))
	for i, p := range c.providers {
		names[i] = p.Name()
	}
	return "chain[" + strings.Join(names, "->") + "]"
}

func (c *ChainProvider) Metadata(ctx context.Context, key string) (SecretMetadata, error) {
	var notFoundErrs []string

	for _, p := range c.providers {
		mp, ok := p.(MetadataProvider)
		if !ok {
			continue
		}
		md, err := mp.Metadata(ctx, key)
		if err == nil {
			return md, nil
		}
		if errors.Is(err, ErrMetadataNotFound) || errors.Is(err, ErrMetadataNotSupported) {
			notFoundErrs = append(notFoundErrs, p.Name())
			continue
		}
		return SecretMetadata{}, fmt.Errorf("provider %q metadata: %w", p.Name(), err)
	}

	return SecretMetadata{}, fmt.Errorf(
		"metadata for %q not found in providers [%s]: %w",
		key,
		strings.Join(notFoundErrs, ", "),
		ErrMetadataNotFound,
	)
}