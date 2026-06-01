package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWKSCache_GetKey(t *testing.T) {
	// Generate a test RSA key
	rawKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	key, err := jwk.FromRaw(rawKey)
	require.NoError(t, err)
	_ = key.Set(jwk.KeyIDKey, "test-kid")
	_ = key.Set(jwk.AlgorithmKey, "RS256")

	// Create a JWKS set
	set := jwk.NewSet()
	_ = set.AddKey(key)

	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		_ = json.NewEncoder(w).Encode(set)
	}))
	defer server.Close()

	cache := NewJWKSCache(server.URL, 100*time.Millisecond)

	// 1. Successful fetch
	k, err := cache.GetKey(context.Background(), "test-kid")
	assert.NoError(t, err)
	assert.Equal(t, "test-kid", k.KeyID())
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))

	// 2. Cache hit (no extra call)
	k, err = cache.GetKey(context.Background(), "test-kid")
	assert.NoError(t, err)
	assert.Equal(t, "test-kid", k.KeyID())
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))

	// 3. Unknown kid (negative cache)
	_, err = cache.GetKey(context.Background(), "unknown-kid")
	assert.Error(t, err)
	// One refresh happens because we look for "unknown-kid" and it's not in the initial set
	// Wait, actually the first call fetched "test-kid", so the set is in cache.
	// Looking for "unknown-kid" will trigger a refresh because it's not found in the cached set.
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))

	// 4. Rate limiting (no extra call for unknown kid within 60s)
	_, err = cache.GetKey(context.Background(), "another-unknown")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))
}

func TestJWKSCache_ExpiredCache(t *testing.T) {
	rawKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	key, _ := jwk.FromRaw(rawKey)
	_ = key.Set(jwk.KeyIDKey, "test-kid")
	set := jwk.NewSet()
	_ = set.AddKey(key)

	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		_ = json.NewEncoder(w).Encode(set)
	}))
	defer server.Close()

	// Short TTL, but refreshLimit will still block immediate refresh
	cache := NewJWKSCache(server.URL, 10*time.Millisecond)
	cache.refreshLimit = 0 // Disable rate limit for this test

	_, _ = cache.GetKey(context.Background(), "test-kid")
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))

	time.Sleep(20 * time.Millisecond)

	_, _ = cache.GetKey(context.Background(), "test-kid")
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))
}

func TestJWKSCache_IdPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cache := NewJWKSCache(server.URL, time.Hour)
	_, err := cache.GetKey(context.Background(), "any-kid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch JWKS")
}

func TestJWKSCache_MalformedPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	cache := NewJWKSCache(server.URL, time.Hour)
	_, err := cache.GetKey(context.Background(), "any-kid")
	assert.Error(t, err)
}

func TestJWKSCache_MixedTokens(t *testing.T) {
	// This is more of a middleware test, but we can verify the cache logic here
	// by ensuring it doesn't fail when requested with different kids.
}
