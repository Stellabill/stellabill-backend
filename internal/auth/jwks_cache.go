package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
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
	defer c.mu.Unlock()

	// Double check after acquiring lock
	if key, found := c.keys[kid]; found && time.Now().Before(c.expiry) {
		return key, nil
	}
	if negExpiry, inNegCache := c.negativeCache[kid]; inNegCache && time.Now().Before(negExpiry) {
		return nil, fmt.Errorf("key id %s not found (negative cached)", kid)
	}

	// Rate limit refreshes
	if time.Since(c.lastRefresh) < c.refreshLimit {
		// If we can't refresh yet, and we don't have the key, return error or stale key
		if key, found := c.keys[kid]; found {
			return key, nil // Return stale key as fallback
		}
		return nil, fmt.Errorf("rate limited: last refresh was %v ago", time.Since(c.lastRefresh))
	}

	if c.url == "" {
		return nil, errors.New("JWKS_URL is not configured")
	}

	// Fetch new JWKS
	set, err := jwk.Fetch(ctx, c.url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	// Update cache
	newKeys := make(map[string]jwk.Key)
	iter := set.Keys(ctx)
	for iter.Next(ctx) {
		k := iter.Pair().Value.(jwk.Key)
		if k.KeyID() != "" {
			newKeys[k.KeyID()] = k
		}
	}

	c.keys = newKeys
	c.expiry = time.Now().Add(c.ttl)
	c.lastRefresh = time.Now()
	
	// Reset negative cache on successful refresh
	c.negativeCache = make(map[string]time.Time)

	// Check if the requested kid is in the new set
	if key, found := c.keys[kid]; found {
		return key, nil
	}

	// If still not found, add to negative cache
	c.negativeCache[kid] = time.Now().Add(c.ttl) // Or a different negative TTL? Let's use ttl.
	return nil, fmt.Errorf("key id %s not found in JWKS", kid)
}
