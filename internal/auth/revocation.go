package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	revokedJTIKeyPrefix = "jwt:revoked:"
)

// RevocationStore represents a store for revoked JWT identifiers.
type RevocationStore interface {
	Revoke(ctx context.Context, jti string, ttl time.Duration) error
	IsRevoked(ctx context.Context, jti string) (bool, error)
}

// RedisRevocationStore implements RevocationStore backed by Redis.
type RedisRevocationStore struct {
	client   redis.UniversalClient
	cache    *localRevocationCache
	failOpen bool
}

// NewRedisRevocationStoreFromURL creates a Redis revocation store from a Redis URL.
func NewRedisRevocationStoreFromURL(url string, failOpen bool) (*RedisRevocationStore, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return NewRedisRevocationStoreFromClient(redis.NewClient(opts), failOpen), nil
}

// NewRedisRevocationStoreFromClient creates a Redis revocation store from an existing client.
func NewRedisRevocationStoreFromClient(client redis.UniversalClient, failOpen bool) *RedisRevocationStore {
	return &RedisRevocationStore{
		client:   client,
		failOpen: failOpen,
		cache:    newLocalRevocationCache(1 * time.Second),
	}
}

// Revoke stores a JTI in Redis with the provided TTL.
func (r *RedisRevocationStore) Revoke(ctx context.Context, jti string, ttl time.Duration) error {
	if jti == "" {
		return errors.New("missing jti")
	}
	if ttl <= 0 {
		return errors.New("expiration must be in the future")
	}
	key := revokedJTIKeyPrefix + jti
	return r.client.Set(ctx, key, "1", ttl).Err()
}

// IsRevoked checks whether the given JTI is revoked.
func (r *RedisRevocationStore) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}

	if r.cache != nil && r.cache.Has(jti) {
		return true, nil
	}

	key := revokedJTIKeyPrefix + jti
	exists, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		if r.failOpen {
			return false, nil
		}
		return false, err
	}

	if exists > 0 {
		if r.cache != nil {
			r.cache.Add(jti)
		}
		return true, nil
	}
	return false, nil
}

// localRevocationCache caches revoked JTI membership for a short duration.
type localRevocationCache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[string]time.Time
}

func newLocalRevocationCache(ttl time.Duration) *localRevocationCache {
	return &localRevocationCache{
		ttl:   ttl,
		items: make(map[string]time.Time),
	}
}

func (c *localRevocationCache) Has(jti string) bool {
	c.mu.RLock()
	exp, ok := c.items[jti]
	c.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().Before(exp) {
		return true
	}

	c.mu.Lock()
	delete(c.items, jti)
	c.mu.Unlock()
	return false
}

func (c *localRevocationCache) Add(jti string) {
	c.mu.Lock()
	c.items[jti] = time.Now().Add(c.ttl)
	c.mu.Unlock()
}
