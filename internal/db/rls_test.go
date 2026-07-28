package db

import (
	"context"
	"stellarbill-backend/internal/middleware"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantIDFromContext_MissingFailsClosed(t *testing.T) {
	ctx := context.Background()

	_, err := TenantIDFromContext(ctx)
	require.ErrorIs(t, err, ErrMissingTenantContext,
		"empty context must fail closed with ErrMissingTenantContext, not open")
}

func TestTenantIDFromContext_EmptyStringFailsClosed(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, DefaultTenantIDKey, "")

	_, err := TenantIDFromContext(ctx)
	require.ErrorIs(t, err, ErrMissingTenantContext,
		"empty string tenant must fail closed, not be accepted")
}

func TestTenantIDFromContext_DefaultKey(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, DefaultTenantIDKey, "tenant-alpha")

	got, err := TenantIDFromContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, "tenant-alpha", got)
}

func TestTenantIDFromContext_MiddlewareKey(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, middleware.TenantIDKey, "tenant-beta")

	got, err := TenantIDFromContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, "tenant-beta", got,
		"middleware.TenantIDKey must be recognized for backward compatibility")
}

func TestTenantIDFromContext_DefaultKeyTakesPrecedence(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, DefaultTenantIDKey, "first-choice")
	ctx = context.WithValue(ctx, middleware.TenantIDKey, "second-choice")

	got, err := TenantIDFromContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, "first-choice", got)
}

func TestContextWithTenantID_SetsBothKeys(t *testing.T) {
	ctx := context.Background()
	ctx = ContextWithTenantID(ctx, "tenant-gamma")

	fromDefault, ok := ctx.Value(DefaultTenantIDKey).(string)
	require.True(t, ok)
	assert.Equal(t, "tenant-gamma", fromDefault)

	fromMiddleware, ok := ctx.Value(middleware.TenantIDKey).(string)
	require.True(t, ok)
	assert.Equal(t, "tenant-gamma", fromMiddleware,
		"ContextWithTenantID must propagate to middleware key for compatibility")
}

func TestContextWithTenantID_ExtractedCorrectly(t *testing.T) {
	original := "tenant-xyz-12345"
	ctx := ContextWithTenantID(context.Background(), original)

	extracted, err := TenantIDFromContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, original, extracted)
}

func TestErrMissingTenantContext_Identifiable(t *testing.T) {
	assert.NotNil(t, ErrMissingTenantContext)
	assert.Contains(t, ErrMissingTenantContext.Error(), "missing tenant context")
}
