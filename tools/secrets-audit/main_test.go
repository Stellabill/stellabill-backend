package main

import (
	"context"
	"testing"
	"time"

	"stellarbill-backend/internal/secrets"

	"github.com/stretchr/testify/require"
)

type fakeMetaProvider struct {
	md  secrets.SecretMetadata
	err error
}

func (f fakeMetaProvider) Metadata(ctx context.Context, key string) (secrets.SecretMetadata, error) {
	return f.md, f.err
}

func (f fakeMetaProvider) GetSecret(ctx context.Context, key string) (string, error) {
	return "", nil
}

func (f fakeMetaProvider) Name() string {
	return "fake"
}

func TestLoadMetadataFallsBackToManifest(t *testing.T) {
	ctx := context.Background()

	manifest := secrets.NewManifestMetadataProvider("testdata/secrets.json")
	provider := fakeMetaProvider{err: secrets.ErrMetadataNotSupported}

	md, err := loadMetadata(ctx, provider, manifest, "JWT_SECRET")
	require.NoError(t, err)
	require.Equal(t, "JWT_SECRET", md.Name)
}

func TestExpiredSecretDetection(t *testing.T) {
	now := time.Now().UTC()
	md := secrets.SecretMetadata{
		Name:              "JWT_SECRET",
		Owner:             "security",
		RotationCadence:   "90d",
		LastRotatedAt:     now.Add(-91 * 24 * time.Hour),
		NextRotationDueAt: now.Add(-24 * time.Hour),
	}

	require.True(t, now.After(md.NextRotationDueAt))
}

func TestMissingMetadataRejected(t *testing.T) {
	_, err := loadMetadata(context.Background(), fakeMetaProvider{err: secrets.ErrMetadataNotFound}, nil, "WEBHOOK_SECRET")
	require.Error(t, err)
}
