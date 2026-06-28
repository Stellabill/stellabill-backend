package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMetadataNotSupported = errors.New("secret metadata not supported")
	ErrMetadataNotFound     = errors.New("secret metadata not found")
)

type SecretMetadata struct {
	Name              string        `json:"name"`
	Owner             string        `json:"owner"`
	Description       string        `json:"description,omitempty"`
	RotationCadence   string        `json:"rotation_cadence"`
	LastRotatedAt     time.Time     `json:"last_rotated_at"`
	NextRotationDueAt time.Time     `json:"next_rotation_due_at"`
	VerificationSteps []string      `json:"verification_steps,omitempty"`
	Source            string        `json:"source,omitempty"`
	Required          bool          `json:"required"`
	RotationInterval  time.Duration `json:"-"`
	GracePeriod       time.Duration `json:"-"`
}

type MetadataProvider interface {
	Metadata(ctx context.Context, key string) (SecretMetadata, error)
}

func ParseDurationLikeRotation(v string) (time.Duration, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return 0, fmt.Errorf("empty duration")
	}

	if strings.HasSuffix(v, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(v, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}

	return time.ParseDuration(v)
}

func dueDateFromLastRotated(last time.Time, cadence time.Duration) time.Time {
	if last.IsZero() || cadence <= 0 {
		return time.Time{}
	}
	return last.Add(cadence)
}

type ManifestMetadataProvider struct {
	path string
}

type manifestFile struct {
	Secrets []SecretMetadata `json:"secrets"`
}

func NewManifestMetadataProvider(path string) *ManifestMetadataProvider {
	return &ManifestMetadataProvider{path: path}
}

func (p *ManifestMetadataProvider) Metadata(ctx context.Context, key string) (SecretMetadata, error) {
	if err := ctx.Err(); err != nil {
		return SecretMetadata{}, err
	}

	if strings.TrimSpace(p.path) == "" {
		return SecretMetadata{}, ErrMetadataNotSupported
	}

	b, err := os.ReadFile(p.path)
	if err != nil {
		return SecretMetadata{}, fmt.Errorf("read metadata manifest: %w", err)
	}

	var mf manifestFile
	if err := json.Unmarshal(b, &mf); err != nil {
		return SecretMetadata{}, fmt.Errorf("parse metadata manifest: %w", err)
	}

	for _, s := range mf.Secrets {
		if s.Name == key {
			if s.Source == "" {
				s.Source = "manifest"
			}
			if s.RotationInterval == 0 && s.RotationCadence != "" {
				if d, err := ParseDurationLikeRotation(s.RotationCadence); err == nil {
					s.RotationInterval = d
				}
			}
			if s.NextRotationDueAt.IsZero() && !s.LastRotatedAt.IsZero() && s.RotationInterval > 0 {
				s.NextRotationDueAt = dueDateFromLastRotated(s.LastRotatedAt, s.RotationInterval)
			}
			return s, nil
		}
	}

	return SecretMetadata{}, fmt.Errorf("%w: %s", ErrMetadataNotFound, key)
}