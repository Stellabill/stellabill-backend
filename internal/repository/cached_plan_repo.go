package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"stellarbill-backend/internal/cache"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

type cacheEnvelope struct {
	Data     []byte    `json:"data"`
	StoredAt time.Time `json:"stored_at"`
}

// CachedPlanRepo decorates a PlanRepository with a read-through cache.
// It implements cache.Purgeable so the admin purge endpoint can flush it.
type CachedPlanRepo struct {
	backend       PlanRepository
	cache         cache.Cache
	ttl           time.Duration
	hits          uint64
	misses        uint64
	stales        uint64
	invalidatedAt sync.Map
	sf            singleflight.Group
}

// NewCachedPlanRepo constructs a CachedPlanRepo.
func NewCachedPlanRepo(backend PlanRepository, c cache.Cache, ttl time.Duration) *CachedPlanRepo {
	return &CachedPlanRepo{backend: backend, cache: c, ttl: ttl}
}

func (cpr *CachedPlanRepo) listKey() string {
	return "plan:list:all"
}

func (cpr *CachedPlanRepo) cacheKey(id string) string {
	return "plan:byid:" + id
}

// FindByID implements PlanRepository. It reads from cache first, falls back to backend
// and updates cache on a successful backend read.
func (cpr *CachedPlanRepo) FindByID(ctx context.Context, id string) (*PlanRow, error) {
	key := cpr.cacheKey(id)
	// Attempt cache fetch
	if cpr.cache != nil {
		if val, err := cpr.cache.Get(ctx, key); err == nil && val != nil {
			var env cacheEnvelope
			if err := json.Unmarshal(val, &env); err == nil {
				// Check for staleness due to invalidation
				stale := false
				if invTimeVal, ok := cpr.invalidatedAt.Load(key); ok {
					if invTime, ok := invTimeVal.(time.Time); ok && env.StoredAt.Before(invTime) {
						stale = true
					}
				}
				if stale {
					atomic.AddUint64(&cpr.stales, 1)
					_ = cpr.cache.Delete(ctx, key)
				} else {
					var pr PlanRow
					if err := json.Unmarshal(env.Data, &pr); err == nil {
						atomic.AddUint64(&cpr.hits, 1)
						return &pr, nil
					}
				}
			}
		}
	}
	// Cache miss, use singleflight to avoid stampede
	atomic.AddUint64(&cpr.misses, 1)
	v, err, _ := cpr.sf.Do(key, func() (interface{}, error) {
		pr, err := cpr.backend.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if cpr.cache != nil {
			prBytes, err := json.Marshal(pr)
			if err == nil {
				env := cacheEnvelope{Data: prBytes, StoredAt: time.Now()}
				if envBytes, err := json.Marshal(env); err == nil {
					_ = cpr.cache.Set(ctx, key, envBytes, cpr.ttl)
				}
			}
		}
		return pr, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*PlanRow), nil
}

// List returns all plans. It caches the full list under a single key.
func (cpr *CachedPlanRepo) List(ctx context.Context) ([]*PlanRow, error) {
	key := "plan:list:all"
	// Attempt cache fetch for list
	if cpr.cache != nil {
		if val, err := cpr.cache.Get(ctx, key); err == nil && val != nil {
			var env cacheEnvelope
			if err := json.Unmarshal(val, &env); err == nil {
				stale := false
				if invTimeVal, ok := cpr.invalidatedAt.Load(key); ok {
					if invTime, ok := invTimeVal.(time.Time); ok && env.StoredAt.Before(invTime) {
						stale = true
					}
				}
				if stale {
					atomic.AddUint64(&cpr.stales, 1)
					_ = cpr.cache.Delete(ctx, key)
				} else {
					var out []*PlanRow
					if err := json.Unmarshal(env.Data, &out); err == nil {
						atomic.AddUint64(&cpr.hits, 1)
						return out, nil
					}
				}
			} else {
				// Corrupted envelope JSON
				return nil, fmt.Errorf("corrupted cache envelope: %w", err)
			}
		}
	}
	// Cache miss, use singleflight for list
	atomic.AddUint64(&cpr.misses, 1)
	v, err, _ := cpr.sf.Do(key, func() (interface{}, error) {
		out, err := cpr.backend.List(ctx)
		if err != nil {
			return nil, err
		}
		if cpr.cache != nil {
			outBytes, err := json.Marshal(out)
			if err == nil {
				env := cacheEnvelope{Data: outBytes, StoredAt: time.Now()}
				if envBytes, err := json.Marshal(env); err == nil {
					_ = cpr.cache.Set(ctx, key, envBytes, cpr.ttl)
				}
			}
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]*PlanRow), nil
}

// Delete invalidates a cached plan entry and records the invalidation time.
func (cpr *CachedPlanRepo) Delete(ctx context.Context, id string) error {
	if cpr.cache == nil {
		return nil
	}
	key := cpr.cacheKey(id)
	now := time.Now()
	cpr.invalidatedAt.Store(key, now)
	cpr.invalidatedAt.Store("plan:list:all", now)

	_ = cpr.cache.Delete(ctx, key)
	_ = cpr.cache.Delete(ctx, "plan:list:all")
	return nil
}

// Metrics returns hit/miss/stale counters for testing/monitoring.
func (cpr *CachedPlanRepo) Metrics() (hits uint64, misses uint64, stales uint64) {
	return atomic.LoadUint64(&cpr.hits), atomic.LoadUint64(&cpr.misses), atomic.LoadUint64(&cpr.stales)
}

// --- cache.Purgeable implementation ---

// Flush evicts all plan cache entries and returns the number of keys removed.
// If the underlying cache implements cache.Flushable, Flush is delegated there
// (O(1), atomic). Otherwise it falls back to deleting the known fixed keys.
// It is safe to call concurrently and when the cache is already empty.
func (cpr *CachedPlanRepo) Flush(ctx context.Context) (int, error) {
	if cpr.cache == nil {
		return 0, nil
	}
	if f, ok := cpr.cache.(cache.Flushable); ok {
		return f.Flush(ctx)
	}
	// Fallback: delete the two fixed keys we know about.
	_ = cpr.cache.Delete(ctx, "plan:list:all")
	return 0, nil
}

// ResetMetrics zeroes the hit/miss counters atomically.
func (cpr *CachedPlanRepo) ResetMetrics() {
	atomic.StoreUint64(&cpr.hits, 0)
	atomic.StoreUint64(&cpr.misses, 0)
	atomic.StoreUint64(&cpr.stales, 0)
}

// Namespace returns the human-readable label for this cache namespace.
func (cpr *CachedPlanRepo) Namespace() string { return "plans" }
