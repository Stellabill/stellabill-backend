package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestKey(kid string) (jwk.Key, error) {
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	key, err := jwk.FromRaw(raw.PublicKey)
	if err != nil {
		return nil, err
	}
	_ = key.Set(jwk.KeyIDKey, kid)
	_ = key.Set(jwk.AlgorithmKey, "RS256")
	return key, nil
}

func TestJWKSCache(t *testing.T) {
	var requestCount int32

	// 1. Setup Mock IDP Server
	key1, _ := generateTestKey("key-1")
	key2, _ := generateTestKey("key-2")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		set := jwk.NewSet()
		
		// Logic to simulate rotation: return different keys based on request count if needed
		// For now, return both keys
		_ = set.AddKey(key1)
		
		// Only add key2 after the second request to test rotation semantics
		if atomic.LoadInt32(&requestCount) > 1 {
			_ = set.AddKey(key2)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	}))
	defer server.Close()

	ctx := context.Background()
	cache := NewJWKSCache(server.URL, 500*time.Millisecond)
	cache.refreshLimit = 0 // Disable rate limit for testing speed

	t.Run("Initial Fetch and Cache Hit", func(t *testing.T) {
		k, err := cache.GetKey(ctx, "key-1")
		require.NoError(t, err)
		assert.Equal(t, "key-1", k.KeyID())
		assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount))

		// Second call should be a cache hit (no extra request)
		k, err = cache.GetKey(ctx, "key-1")
		require.NoError(t, err)
		assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount))
		assert.Equal(t, uint64(1), cache.GetMetrics().Hits)
	})

	t.Run("Refresh on unknown KID (Rotation)", func(t *testing.T) {
		// key-2 is not in cache yet. This should trigger a Refresh.
		k, err := cache.GetKey(ctx, "key-2")
		require.NoError(t, err)
		assert.Equal(t, "key-2", k.KeyID())
		assert.Equal(t, int32(2), atomic.LoadInt32(&requestCount))
	})

	t.Run("TTL Expiry Refresh", func(t *testing.T) {
		time.Sleep(600 * time.Millisecond) // Wait for TTL to expire
		_, err := cache.Get(ctx)
		require.NoError(t, err)
		assert.Equal(t, int32(3), atomic.LoadInt32(&requestCount))
	})

	t.Run("Resilience on IDP Failure", func(t *testing.T) {
		// Point to invalid URL
		badCache := NewJWKSCache("http://localhost:1", 1*time.Hour)
		_, err := badCache.Get(ctx)
		assert.Error(t, err)
		assert.Equal(t, uint64(1), badCache.GetMetrics().RefreshFailures)
	})
}
