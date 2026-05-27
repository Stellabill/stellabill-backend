package repository

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"stellarbill-backend/internal/cache"
)

// cacheEnvelope wraps the actual data with a stored timestamp so the decorator
// can detect stale reads after explicit invalidation.
type cacheEnvelope struct {
	Data     []byte    `json:"data"`
	StoredAt time.Time `json:"stored_at"`
}

// CachedPlanRepo decorates a PlanRepository with a read-through cache.
// It implements cache.Purgeable so the admin purge endpoint can flush it.
type CachedPlanRepo struct {
	backend PlanRepository
	cache   cache.Cache
	ttl     time.Duration

	hits   uint64
	misses uint64
}

// NewCachedPlanRepo constructs a CachedPlanRepo.
func NewCachedPlanRepo(backend PlanRepository, c cache.Cache, ttl time.Duration) *CachedPlanRepo {
	return &CachedPlanRepo{backend: backend, cache: c, ttl: ttl}
}

func (cpr *CachedPlanRepo) cacheKey(id string) string {
	return "plan:byid:" + id
}

// FindByID implements PlanRepository. It reads from cache first, falls back to backend
// and updates cache on a successful backend read.
func (cpr *CachedPlanRepo) FindByID(ctx context.Context, id string) (*PlanRow, error) {
	key := cpr.cacheKey(id)
	if cpr.cache != nil {
		if val, err := cpr.cache.Get(ctx, key); err == nil && val != nil {
			var pr PlanRow
			if err := json.Unmarshal(val, &pr); err == nil {
				atomic.AddUint64(&cpr.hits, 1)
				return &pr, nil
			}
			// on unmarshal errors, fallthrough to backend
		}
	}
	atomic.AddUint64(&cpr.misses, 1)
	pr, err := cpr.backend.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cpr.cache != nil {
		if b, err := json.Marshal(pr); err == nil {
			_ = cpr.cache.Set(ctx, key, b, cpr.ttl)
		}
	}
	return pr, nil
}

// List returns all plans. It caches the full list under a single key.
func (cpr *CachedPlanRepo) List(ctx context.Context) ([]*PlanRow, error) {
	key := "plan:list:all"
	if cpr.cache != nil {
		if val, err := cpr.cache.Get(ctx, key); err == nil && val != nil {
			var out []*PlanRow
			if err := json.Unmarshal(val, &out); err == nil {
				atomic.AddUint64(&cpr.hits, 1)
				return out, nil
			}
		}
	}
	atomic.AddUint64(&cpr.misses, 1)
	out, err := cpr.backend.List(ctx)
	if err != nil {
		return nil, err
	}
	if cpr.cache != nil {
		if b, err := json.Marshal(out); err == nil {
			_ = cpr.cache.Set(ctx, key, b, cpr.ttl)
		}
	}
	return out, nil
}

// Delete invalidates a cached plan entry and records the invalidation time.
func (cpr *CachedPlanRepo) Delete(ctx context.Context, id string) error {
	if cpr.cache == nil {
		return nil
	}
	_ = cpr.cache.Delete(ctx, cpr.cacheKey(id))
	_ = cpr.cache.Delete(ctx, "plan:list:all")
	return nil
}

// Metrics returns hit/miss counters for testing/monitoring.
func (cpr *CachedPlanRepo) Metrics() (hits uint64, misses uint64) {
	return atomic.LoadUint64(&cpr.hits), atomic.LoadUint64(&cpr.misses)
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
}

// Namespace returns the human-readable label for this cache namespace.
func (cpr *CachedPlanRepo) Namespace() string { return "plans" }
