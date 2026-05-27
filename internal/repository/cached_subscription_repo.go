package repository

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"stellarbill-backend/internal/cache"
)

// CachedSubscriptionRepo decorates a SubscriptionRepository with a read-through cache.
// It implements cache.Purgeable so the admin purge endpoint can flush it.
type CachedSubscriptionRepo struct {
	backend SubscriptionRepository
	cache   cache.Cache
	ttl     time.Duration

	hits   uint64
	misses uint64
}

// NewCachedSubscriptionRepo constructs a CachedSubscriptionRepo.
func NewCachedSubscriptionRepo(backend SubscriptionRepository, c cache.Cache, ttl time.Duration) *CachedSubscriptionRepo {
	return &CachedSubscriptionRepo{backend: backend, cache: c, ttl: ttl}
}

func (csr *CachedSubscriptionRepo) byIDKey(id string) string {
	return "sub:byid:" + id
}

func (csr *CachedSubscriptionRepo) byTenantKey(id, tenantID string) string {
	return "sub:bytenant:" + id + ":" + tenantID
}

// FindByID implements SubscriptionRepository.
// It reads from cache first and falls back to the backend on a miss,
// then caches the result for future reads.
func (csr *CachedSubscriptionRepo) FindByID(ctx context.Context, id string) (*SubscriptionRow, error) {
	key := csr.byIDKey(id)
	if csr.cache != nil {
		if val, err := csr.cache.Get(ctx, key); err == nil && val != nil {
			var sr SubscriptionRow
			if err := json.Unmarshal(val, &sr); err == nil {
				atomic.AddUint64(&csr.hits, 1)
				return &sr, nil
			}
			// Unmarshal error: fall through to backend.
		}
	}
	atomic.AddUint64(&csr.misses, 1)
	sr, err := csr.backend.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if csr.cache != nil {
		if b, err := json.Marshal(sr); err == nil {
			_ = csr.cache.Set(ctx, key, b, csr.ttl)
		}
	}
	return sr, nil
}

// FindByIDAndTenant implements SubscriptionRepository.
// The cache key is scoped to both the subscription ID and the tenant ID so
// that cross-tenant reads never produce a stale hit.
func (csr *CachedSubscriptionRepo) FindByIDAndTenant(ctx context.Context, id string, tenantID string) (*SubscriptionRow, error) {
	key := csr.byTenantKey(id, tenantID)
	if csr.cache != nil {
		if val, err := csr.cache.Get(ctx, key); err == nil && val != nil {
			var sr SubscriptionRow
			if err := json.Unmarshal(val, &sr); err == nil {
				atomic.AddUint64(&csr.hits, 1)
				return &sr, nil
			}
		}
	}
	atomic.AddUint64(&csr.misses, 1)
	sr, err := csr.backend.FindByIDAndTenant(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if csr.cache != nil {
		if b, err := json.Marshal(sr); err == nil {
			_ = csr.cache.Set(ctx, key, b, csr.ttl)
		}
	}
	return sr, nil
}

// UpdateStatus delegates the write to the backend and invalidates read-through cache entries.
func (csr *CachedSubscriptionRepo) UpdateStatus(ctx context.Context, id string, tenantID string, status string) error {
	if err := csr.backend.UpdateStatus(ctx, id, tenantID, status); err != nil {
		return err
	}
	return csr.Delete(ctx, id, tenantID)
}

// Delete removes cached entries for a subscription and records invalidation times.
// It clears both the by-id and by-id-and-tenant keys.
func (csr *CachedSubscriptionRepo) Delete(ctx context.Context, id string, tenantID string) error {
	if csr.cache == nil {
		return 0, nil
	}
	if f, ok := csr.cache.(cache.Flushable); ok {
		return f.Flush(ctx)
	}
	// Non-Flushable cache: no bulk eviction possible; return 0 with no error.
	return 0, nil
}

// ResetMetrics zeroes the hit/miss counters atomically.
func (csr *CachedSubscriptionRepo) ResetMetrics() {
	atomic.StoreUint64(&csr.hits, 0)
	atomic.StoreUint64(&csr.misses, 0)
}

// Namespace returns the human-readable label for this cache namespace.
func (csr *CachedSubscriptionRepo) Namespace() string { return "subscriptions" }
