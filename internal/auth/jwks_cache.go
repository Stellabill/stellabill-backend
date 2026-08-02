package auth

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"golang.org/x/sync/singleflight"
)

const (
	// maxNegativeCacheTTL caps how long an absent/unknown kid is cached as a
	// miss, regardless of the cache's configured (positive) ttl. This bounds
	// how long a key that legitimately rotates in (e.g. during an issuer key
	// rollover) can be masked by a stale negative-cache entry.
	maxNegativeCacheTTL = 5 * time.Second

	// negativeCacheJitter randomizes the negative TTL within
	// (maxNegativeCacheTTL-negativeCacheJitter, maxNegativeCacheTTL], so many
	// callers that cache a miss for the same kid at the same instant don't
	// all expire and refetch in the same instant.
	negativeCacheJitter = 1 * time.Second
)

// JWKSCache implements a JWKS key cache with TTL, negative caching, and rate limiting.
type JWKSCache struct {
	mu            sync.RWMutex
	url           string
	ttl           time.Duration
	keys          map[string]jwk.Key
	negativeCache map[string]time.Time
	expiry        time.Time
	lastRefresh   time.Time
	refreshLimit  time.Duration
	sf            singleflight.Group
}

// NewJWKSCache initializes a new JWKSCache.
func NewJWKSCache(url string, ttl time.Duration) *JWKSCache {
	return &JWKSCache{
		url:           url,
		ttl:           ttl,
		keys:          make(map[string]jwk.Key),
		negativeCache: make(map[string]time.Time),
		refreshLimit:  60 * time.Second,
	}
}

// negativeTTL returns a jittered TTL capped at maxNegativeCacheTTL.
func negativeTTL() time.Duration {
	jitter := time.Duration(rand.Int63n(int64(negativeCacheJitter)))
	return maxNegativeCacheTTL - jitter
}

// GetKey retrieves a public key by kid.
func (c *JWKSCache) GetKey(ctx context.Context, kid string) (jwk.Key, error) {
	if kid == "" {
		return nil, errors.New("kid is required")
	}

	// 1. Try to get from cache
	c.mu.RLock()
	key, found := c.keys[kid]
	isExpired := time.Now().After(c.expiry)

	// Check negative cache
	negExpiry, inNegCache := c.negativeCache[kid]
	isNegExpired := time.Now().After(negExpiry)
	c.mu.RUnlock()

	if found && !isExpired {
		return key, nil
	}

	if inNegCache && !isNegExpired {
		return nil, fmt.Errorf("key id %s not found (negative cached)", kid)
	}

	// 2. Refresh if needed
	return c.refreshAndGetKey(ctx, kid)
}

func (c *JWKSCache) refreshAndGetKey(ctx context.Context, kid string) (jwk.Key, error) {
	c.mu.Lock()

	// Double check after acquiring lock
	if key, found := c.keys[kid]; found && time.Now().Before(c.expiry) {
		c.mu.Unlock()
		return key, nil
	}
	if negExpiry, inNegCache := c.negativeCache[kid]; inNegCache && time.Now().Before(negExpiry) {
		c.mu.Unlock()
		return nil, fmt.Errorf("key id %s not found (negative cached)", kid)
	}

	// Rate limit refreshes
	if time.Since(c.lastRefresh) < c.refreshLimit {
		// If we can't refresh yet, and we don't have the key, return error or stale key
		if key, found := c.keys[kid]; found {
			c.mu.Unlock()
			return key, nil // Return stale key as fallback
		}
		sinceRefresh := time.Since(c.lastRefresh)
		c.mu.Unlock()
		return nil, fmt.Errorf("rate limited: last refresh was %v ago", sinceRefresh)
	}

	if c.url == "" {
		c.mu.Unlock()
		return nil, errors.New("JWKS_URL is not configured")
	}

	// Release the lock before doing network I/O. Holding it here would block
	// GetKey for every other (already-cached, healthy) kid for the entire
	// round trip to the issuer — exactly the "one broken issuer degrades the
	// whole cluster" failure mode this change is meant to prevent.
	c.mu.Unlock()

	// Coalesce concurrent refetches for this URL into a single in-flight
	// fetch, so a burst of simultaneous cache misses never sends more than
	// one request to the issuer at a time.
	result, err, _ := c.sf.Do(c.url, func() (interface{}, error) {
		set, fetchErr := jwk.Fetch(ctx, c.url)
		if fetchErr != nil {
			jwksRefreshErrorsTotal.Inc()
			return nil, fmt.Errorf("failed to fetch JWKS: %w", fetchErr)
		}

		newKeys := make(map[string]jwk.Key)
		iter := set.Keys(ctx)
		for iter.Next(ctx) {
			k := iter.Pair().Value.(jwk.Key)
			if k.KeyID() != "" {
				newKeys[k.KeyID()] = k
			}
		}
		return newKeys, nil
	})

	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		// Always record the attempt, even on failure, so a persistently
		// broken issuer is still rate-limited rather than hit on every
		// single request. Previously lastRefresh was only updated on
		// success, so an issuer that always errors was never rate-limited.
		c.lastRefresh = time.Now()
		return nil, err
	}

	newKeys := result.(map[string]jwk.Key)
	c.keys = newKeys
	c.expiry = time.Now().Add(c.ttl)
	c.lastRefresh = time.Now()

	// Reset negative cache on successful refresh
	c.negativeCache = make(map[string]time.Time)

	// Check if the requested kid is in the new set
	if key, found := c.keys[kid]; found {
		return key, nil
	}

	// If still not found, add to negative cache, capped at maxNegativeCacheTTL
	// regardless of the configured (positive) ttl.
	c.negativeCache[kid] = time.Now().Add(negativeTTL())
	return nil, fmt.Errorf("key id %s not found in JWKS", kid)
}