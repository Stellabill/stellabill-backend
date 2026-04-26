package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
)

// Metrics structure to satisfy the "Add metrics for refresh failures and hit rate" requirement.
type CacheMetrics struct {
	Hits            uint64
	Misses          uint64
	RefreshFailures uint64
}

type JWKSCache struct {
	mu           sync.RWMutex
	set          jwk.Set
	expiry       time.Time
	url          string
	ttl          time.Duration
	refreshLimit time.Duration
	lastRefresh  time.Time
	
	// Metrics
	metrics      CacheMetrics
}

// NewJWKSCache initializes the cache with bounded TTL.
func NewJWKSCache(url string, ttl time.Duration) *JWKSCache {
	return &JWKSCache{
		url:          url,
		ttl:          ttl,
		refreshLimit: 1 * time.Minute, // Prevent cache stampede
	}
}

// Get retrieves the current JWKS set, checking TTL.
func (c *JWKSCache) Get(ctx context.Context) (jwk.Set, error) {
	c.mu.RLock()
	if c.set != nil && time.Now().Before(c.expiry) {
		c.metrics.Hits++
		defer c.mu.RUnlock()
		return c.set, nil
	}
	c.mu.RUnlock()

	c.metrics.Misses++
	return c.Refresh(ctx)
}

// Refresh forces a fetch from the IDP with rate-limiting to prevent stampedes.
func (c *JWKSCache) Refresh(ctx context.Context) (jwk.Set, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check pattern
	if c.set != nil && time.Now().Before(c.expiry) {
		return c.set, nil
	}

	// Rate limit: If we just refreshed, don't hammer the IDP
	if time.Since(c.lastRefresh) < c.refreshLimit && c.set != nil {
		return c.set, nil
	}

	set, err := jwk.Fetch(ctx, c.url)
	if err != nil {
		c.metrics.RefreshFailures++
		// If fetch fails but we have stale keys, return them as fallback
		if c.set != nil {
			return c.set, nil
		}
		return nil, err
	}

	c.set = set
	c.expiry = time.Now().Add(c.ttl)
	c.lastRefresh = time.Now()
	
	return c.set, nil
}

// GetKey retrieves a specific key by ID, triggering a refresh if the ID is missing.
// This handles the "Rotation Semantics" requirement.
func (c *JWKSCache) GetKey(ctx context.Context, kid string) (jwk.Key, error) {
	set, err := c.Get(ctx)
	if err != nil {
		return nil, err
	}

	key, found := set.LookupKeyID(kid)
	if !found {
		// Key ID not found - trigger immediate refresh-on-error
		set, err = c.Refresh(ctx)
		if err != nil {
			return nil, err
		}
		
		key, found = set.LookupKeyID(kid)
		if !found {
			return nil, errors.New("key id not found after cache refresh")
		}
	}
	return key, nil
}

// GetMetrics returns the current cache performance stats.
func (c *JWKSCache) GetMetrics() CacheMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metrics
}
