package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// EnvProvider tests
// ---------------------------------------------------------------------------

func TestEnvProvider_GetSecret_HappyPath(t *testing.T) {
	t.Setenv("TEST_SECRET_A", "hunter2")

	p := NewEnvProvider()
	val, err := p.GetSecret(context.Background(), "TEST_SECRET_A")
	require.NoError(t, err)
	assert.Equal(t, "hunter2", val)
}

func TestEnvProvider_GetSecret_MissingKey(t *testing.T) {
	p := NewEnvProvider()
	_, err := p.GetSecret(context.Background(), "TOTALLY_MISSING_KEY_XYZ")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSecretNotFound))
}

func TestEnvProvider_GetSecret_EmptyValue(t *testing.T) {
	t.Setenv("TEST_EMPTY_SECRET", "")

	p := NewEnvProvider()
	_, err := p.GetSecret(context.Background(), "TEST_EMPTY_SECRET")
	assert.True(t, errors.Is(err, ErrSecretNotFound))
}

func TestEnvProvider_GetSecret_EmptyKey(t *testing.T) {
	p := NewEnvProvider()
	_, err := p.GetSecret(context.Background(), "")
	assert.True(t, errors.Is(err, ErrSecretNotFound))
}

func TestEnvProvider_GetSecret_WhitespaceKey(t *testing.T) {
	p := NewEnvProvider()
	_, err := p.GetSecret(context.Background(), "   ")
	assert.True(t, errors.Is(err, ErrSecretNotFound))
}

func TestEnvProvider_GetSecret_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := NewEnvProvider()
	_, err := p.GetSecret(ctx, "ANY_KEY")
	assert.True(t, errors.Is(err, ErrProviderTimeout))
}

func TestEnvProvider_WithPrefix(t *testing.T) {
	t.Setenv("MYAPP_DB_PASSWORD", "secret123")

	p := NewEnvProviderWithPrefix("MYAPP_")
	val, err := p.GetSecret(context.Background(), "DB_PASSWORD")
	require.NoError(t, err)
	assert.Equal(t, "secret123", val)
}

func TestEnvProvider_Name(t *testing.T) {
	assert.Equal(t, "env", NewEnvProvider().Name())
	assert.Equal(t, "env:MYAPP_", NewEnvProviderWithPrefix("MYAPP_").Name())
}

// ---------------------------------------------------------------------------
// SafeValue tests
// ---------------------------------------------------------------------------

func TestSafeValue_Expose(t *testing.T) {
	sv := NewSafeValue("super-secret")
	assert.Equal(t, "super-secret", sv.Expose())
}

func TestSafeValue_String_Redacts(t *testing.T) {
	sv := NewSafeValue("super-secret")
	assert.Equal(t, "***REDACTED***", sv.String())
	assert.Equal(t, "***REDACTED***", fmt.Sprintf("%s", sv))
}

func TestSafeValue_GoString_Redacts(t *testing.T) {
	sv := NewSafeValue("super-secret")
	assert.Contains(t, fmt.Sprintf("%#v", sv), "REDACTED")
}

func TestSafeValue_MarshalJSON_Redacts(t *testing.T) {
	sv := NewSafeValue("super-secret")
	b, err := json.Marshal(sv)
	require.NoError(t, err)
	assert.Equal(t, `"***REDACTED***"`, string(b))
}

func TestSafeValue_MarshalJSON_InStruct(t *testing.T) {
	type cfg struct {
		Password SafeValue `json:"password"`
	}
	c := cfg{Password: NewSafeValue("super-secret")}
	b, err := json.Marshal(c)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "super-secret")
	assert.Contains(t, string(b), "REDACTED")
}

func TestSafeValue_IsEmpty(t *testing.T) {
	assert.True(t, NewSafeValue("").IsEmpty())
	assert.False(t, NewSafeValue("x").IsEmpty())
}

// ---------------------------------------------------------------------------
// ChainProvider tests
// ---------------------------------------------------------------------------

// stubProvider is a test double that returns a fixed value or error.
type stubProvider struct {
	name string
	val  string
	err  error
}

func (s *stubProvider) GetSecret(_ context.Context, _ string) (string, error) {
	return s.val, s.err
}

func (s *stubProvider) Name() string { return s.name }

func TestChainProvider_NewChainProvider_Empty(t *testing.T) {
	_, err := NewChainProvider()
	require.Error(t, err)
}

func TestChainProvider_FirstProviderWins(t *testing.T) {
	p1 := &stubProvider{name: "p1", val: "from-p1"}
	p2 := &stubProvider{name: "p2", val: "from-p2"}

	chain, err := NewChainProvider(p1, p2)
	require.NoError(t, err)

	val, err := chain.GetSecret(context.Background(), "key")
	require.NoError(t, err)
	assert.Equal(t, "from-p1", val)
}

func TestChainProvider_FallbackOnNotFound(t *testing.T) {
	p1 := &stubProvider{name: "p1", err: fmt.Errorf("nope: %w", ErrSecretNotFound)}
	p2 := &stubProvider{name: "p2", val: "from-p2"}

	chain, err := NewChainProvider(p1, p2)
	require.NoError(t, err)

	val, err := chain.GetSecret(context.Background(), "key")
	require.NoError(t, err)
	assert.Equal(t, "from-p2", val)
}

func TestChainProvider_AllNotFound(t *testing.T) {
	p1 := &stubProvider{name: "p1", err: ErrSecretNotFound}
	p2 := &stubProvider{name: "p2", err: fmt.Errorf("missing: %w", ErrSecretNotFound)}

	chain, err := NewChainProvider(p1, p2)
	require.NoError(t, err)

	_, err = chain.GetSecret(context.Background(), "key")
	assert.True(t, errors.Is(err, ErrSecretNotFound))
}

func TestChainProvider_NonNotFoundError_StopsImmediately(t *testing.T) {
	p1 := &stubProvider{name: "p1", err: fmt.Errorf("network failure")}
	p2 := &stubProvider{name: "p2", val: "should-not-reach"}

	chain, err := NewChainProvider(p1, p2)
	require.NoError(t, err)

	_, err = chain.GetSecret(context.Background(), "key")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrSecretNotFound))
	assert.Contains(t, err.Error(), "network failure")
	assert.Contains(t, err.Error(), "p1")
}

func TestChainProvider_TimeoutError_StopsImmediately(t *testing.T) {
	p1 := &stubProvider{name: "p1", err: ErrProviderTimeout}
	p2 := &stubProvider{name: "p2", val: "should-not-reach"}

	chain, err := NewChainProvider(p1, p2)
	require.NoError(t, err)

	_, err = chain.GetSecret(context.Background(), "key")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProviderTimeout))
}

func TestChainProvider_Name(t *testing.T) {
	p1 := &stubProvider{name: "env"}
	p2 := &stubProvider{name: "vault"}

	chain, err := NewChainProvider(p1, p2)
	require.NoError(t, err)

	assert.Equal(t, "chain[env->vault]", chain.Name())
}

// ---------------------------------------------------------------------------
// Concurrency safety
// ---------------------------------------------------------------------------

func TestEnvProvider_ConcurrentAccess(t *testing.T) {
	t.Setenv("CONCURRENT_SECRET", "value")

	p := NewEnvProvider()
	var wg sync.WaitGroup
	errs := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			val, err := p.GetSecret(context.Background(), "CONCURRENT_SECRET")
			if err != nil {
				errs <- err
				return
			}
			if val != "value" {
				errs <- fmt.Errorf("unexpected value: %s", val)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent access error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestEnvProvider_VeryLongValue(t *testing.T) {
	long := make([]byte, 10000)
	for i := range long {
		long[i] = 'A'
	}
	t.Setenv("LONG_SECRET", string(long))

	p := NewEnvProvider()
	val, err := p.GetSecret(context.Background(), "LONG_SECRET")
	require.NoError(t, err)
	assert.Len(t, val, 10000)
}

func TestSafeValue_ZeroValue(t *testing.T) {
	var sv SafeValue
	assert.Equal(t, "***REDACTED***", sv.String())
	assert.Equal(t, "", sv.Expose())
	assert.True(t, sv.IsEmpty())
}

