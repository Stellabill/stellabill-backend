package repository

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"stellarbill-backend/internal/cache"
)

// CachedSubscriptionRepo decorates a SubscriptionRepository with a read-through cache.
type CachedSubscriptionRepo struct {
	backend SubscriptionRepository
	cache   cache.Cache
	guard   *cache.GuardedCache
	ttl     time.Duration

	hits   uint64
	misses uint64
	stales uint64

	invalidatedMu sync.RWMutex
	invalidatedAt map[string]time.Time
}

// NewCachedSubscriptionRepo constructs a CachedSubscriptionRepo.
func NewCachedSubscriptionRepo(backend SubscriptionRepository, c cache.Cache, ttl time.Duration) *CachedSubscriptionRepo {
	return &CachedSubscriptionRepo{
		backend:       backend,
		cache:         c,
		guard:         cache.NewGuardedCache(c),
		ttl:           ttl,
		invalidatedAt: make(map[string]time.Time),
	}
}

func (csr *CachedSubscriptionRepo) cacheKey(id string) string {
	return "sub:byid:" + id
}

func (csr *CachedSubscriptionRepo) tenantCacheKey(id string, tenantID string) string {
	return "sub:byidandtenant:" + id + ":" + tenantID
}

// isStale returns true if the envelope was stored before the last invalidation of key.
func (csr *CachedSubscriptionRepo) isStale(key string, env cacheEnvelope) bool {
	csr.invalidatedMu.RLock()
	t, ok := csr.invalidatedAt[key]
	csr.invalidatedMu.RUnlock()
	return ok && env.StoredAt.Before(t)
}

// readEnvelope attempts to load and unmarshal a cacheEnvelope for key.
// It returns (nil, false) on cache miss or error.
func (csr *CachedSubscriptionRepo) readEnvelope(ctx context.Context, key string) (*cacheEnvelope, bool) {
	if csr.cache == nil {
		return nil, false
	}
	val, err := csr.cache.Get(ctx, key)
	if err != nil || val == nil {
		return nil, false
	}
	var env cacheEnvelope
	if err := json.Unmarshal(val, &env); err != nil {
		return nil, false
	}
	return &env, true
}

// FindByID implements SubscriptionRepository. It reads from cache first, falls back to backend
// and updates cache on a successful backend read.
func (csr *CachedSubscriptionRepo) FindByID(ctx context.Context, id string) (*SubscriptionRow, error) {
	key := csr.cacheKey(id)

	// Fast path: fresh cache hit
	if env, ok := csr.readEnvelope(ctx, key); ok && !csr.isStale(key, *env) {
		var sr SubscriptionRow
		if err := json.Unmarshal(env.Data, &sr); err == nil {
			atomic.AddUint64(&csr.hits, 1)
			return &sr, nil
		}
		// Inner data corrupt; purge so GetOrLoad refreshes
		_ = csr.cache.Delete(ctx, key)
	}

	// Stale path: cached but invalidated — purge so GetOrLoad loads fresh
	if env, ok := csr.readEnvelope(ctx, key); ok && csr.isStale(key, *env) {
		atomic.AddUint64(&csr.stales, 1)
		_ = csr.cache.Delete(ctx, key)
	}

	// Miss or stale-removed path: guarded load from backend
	atomic.AddUint64(&csr.misses, 1)
	envelopeBytes, err := csr.guard.GetOrLoad(ctx, key, csr.ttl, func() ([]byte, error) {
		sr, err := csr.backend.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(sr)
		if err != nil {
			return nil, err
		}
		env := cacheEnvelope{Data: data, StoredAt: time.Now()}
		return json.Marshal(env)
	})
	if err != nil {
		return nil, err
	}

	var env cacheEnvelope
	if err := json.Unmarshal(envelopeBytes, &env); err != nil {
		return nil, err
	}
	var sr SubscriptionRow
	if err := json.Unmarshal(env.Data, &sr); err != nil {
		return nil, err
	}
	return &sr, nil
}

// FindByIDAndTenant implements SubscriptionRepository with tenant-scoped caching.
func (csr *CachedSubscriptionRepo) FindByIDAndTenant(ctx context.Context, id string, tenantID string) (*SubscriptionRow, error) {
	key := csr.tenantCacheKey(id, tenantID)

	// Fast path: fresh cache hit
	if env, ok := csr.readEnvelope(ctx, key); ok && !csr.isStale(key, *env) {
		var sr SubscriptionRow
		if err := json.Unmarshal(env.Data, &sr); err == nil {
			atomic.AddUint64(&csr.hits, 1)
			return &sr, nil
		}
		// Inner data corrupt; purge so GetOrLoad refreshes
		_ = csr.cache.Delete(ctx, key)
	}

	// Stale path: cached but invalidated — purge so GetOrLoad loads fresh
	if env, ok := csr.readEnvelope(ctx, key); ok && csr.isStale(key, *env) {
		atomic.AddUint64(&csr.stales, 1)
		_ = csr.cache.Delete(ctx, key)
	}

	// Miss or stale-removed path: guarded load from backend
	atomic.AddUint64(&csr.misses, 1)
	envelopeBytes, err := csr.guard.GetOrLoad(ctx, key, csr.ttl, func() ([]byte, error) {
		sr, err := csr.backend.FindByIDAndTenant(ctx, id, tenantID)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(sr)
		if err != nil {
			return nil, err
		}
		env := cacheEnvelope{Data: data, StoredAt: time.Now()}
		return json.Marshal(env)
	})
	if err != nil {
		return nil, err
	}

	var env cacheEnvelope
	if err := json.Unmarshal(envelopeBytes, &env); err != nil {
		return nil, err
	}
	var sr SubscriptionRow
	if err := json.Unmarshal(env.Data, &sr); err != nil {
		return nil, err
	}
	return &sr, nil
}

// Delete removes cached entries for a subscription and records invalidation times.
// It clears both the by-id and by-id-and-tenant keys.
func (csr *CachedSubscriptionRepo) Delete(ctx context.Context, id string, tenantID string) error {
	if csr.cache == nil {
		return nil
	}
	key := csr.cacheKey(id)
	tenantKey := csr.tenantCacheKey(id, tenantID)

	_ = csr.guard.Delete(ctx, key)
	_ = csr.guard.Delete(ctx, tenantKey)

	now := time.Now()
	csr.invalidatedMu.Lock()
	csr.invalidatedAt[key] = now
	csr.invalidatedAt[tenantKey] = now
	csr.invalidatedMu.Unlock()
	return nil
}

// Metrics returns hit/miss/stale counters for testing/monitoring.
func (csr *CachedSubscriptionRepo) Metrics() (hits uint64, misses uint64, stales uint64) {
	return atomic.LoadUint64(&csr.hits),
		atomic.LoadUint64(&csr.misses),
		atomic.LoadUint64(&csr.stales)
}
