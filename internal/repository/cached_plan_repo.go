package repository

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"stellabill-backend/internal/cache"
)

// CachedPlanRepo decorates a PlanRepository with a read-through cache.
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
	// fetch from backend
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

// Delete invalidates a cached plan entry.
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

