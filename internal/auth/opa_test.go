package auth

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOPAPolicyEngine(t *testing.T) {
	tmpDir := t.TempDir()
	policyPath := tmpDir + "/admin.rego"

	policyContent := `package admin.authz
default allow = false
allow {
	input.token == "valid-token"
}`
	err := os.WriteFile(policyPath, []byte(policyContent), 0644)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	engine, err := NewOPAPolicyEngine(ctx, policyPath, "")
	require.NoError(t, err)

	// test valid
	allow, err := engine.Authorize(ctx, "valid-token")
	require.NoError(t, err)
	assert.True(t, allow)

	// test invalid
	allow, err = engine.Authorize(ctx, "invalid-token")
	require.NoError(t, err)
	assert.False(t, allow)

	// test missing policy file fail closed
	err = os.Remove(policyPath)
	require.NoError(t, err)

	// reload
	err = engine.loadPolicy(ctx)
	require.Error(t, err)

	allow, err = engine.Authorize(ctx, "valid-token")
	require.NoError(t, err)
	assert.False(t, allow)
}
