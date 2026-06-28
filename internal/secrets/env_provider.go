package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type EnvProvider struct {
	prefix string
}

func NewEnvProvider() *EnvProvider {
	return &EnvProvider{}
}

func NewEnvProviderWithPrefix(prefix string) *EnvProvider {
	return &EnvProvider{prefix: prefix}
}

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

func (p *EnvProvider) Name() string {
	if p.prefix != "" {
		return "env:" + p.prefix
	}
	return "env"
}

func (p *EnvProvider) Metadata(ctx context.Context, key string) (SecretMetadata, error) {
	if err := ctx.Err(); err != nil {
		return SecretMetadata{}, err
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return SecretMetadata{}, ErrMetadataNotFound
	}

	envKey := p.prefix + key + "_ROTATION_METADATA"
	raw := os.Getenv(envKey)
	if raw == "" {
		return SecretMetadata{}, ErrMetadataNotFound
	}

	var md SecretMetadata
	if err := json.Unmarshal([]byte(raw), &md); err != nil {
		return SecretMetadata{}, fmt.Errorf("parse %s: %w", envKey, err)
	}

	if md.Name == "" {
		md.Name = key
	}
	if md.Source == "" {
		md.Source = p.Name()
	}
	if md.RotationCadence != "" && md.RotationInterval == 0 {
		if d, err := ParseDurationLikeRotation(md.RotationCadence); err == nil {
			md.RotationInterval = d
		}
	}
	if md.NextRotationDueAt.IsZero() && !md.LastRotatedAt.IsZero() && md.RotationInterval > 0 {
		md.NextRotationDueAt = md.LastRotatedAt.Add(md.RotationInterval)
	}
	if md.VerificationSteps == nil {
		md.VerificationSteps = []string{}
	}

	return md, nil
}