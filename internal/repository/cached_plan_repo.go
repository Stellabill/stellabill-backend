package repository

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"stellarbill-backend/internal/cache"
	"stellarbill-backend/internal/metrics"
)

// cacheEnvelope wraps the actual data with a stored timestamp so the decorator
// can detect stale reads after explicit invalidation.
type cacheEnvelope struct {
	Data     []byte    `json:"data"`
	StoredAt time.Time `json:"stored_at"`
}

// CachedPlanRepo decorates a PlanRepository with a read-through cache.
type CachedPlanRepo struct {
	backend PlanRepository
	cache   cache.Cache
	guard   *cache.GuardedCache
	ttl     time.Duration

	hits   uint64
	misses uint64
	stales uint64

	invalidatedMu sync.RWMutex
	invalidatedAt map[string]time.Time
}

// NewCachedPlanRepo constructs a CachedPlanRepo.
func NewCachedPlanRepo(backend PlanRepository, c cache.Cache, ttl time.Duration) *CachedPlanRepo {
	return &CachedPlanRepo{
		backend:       backend,
		cache:         c,
		guard:         cache.NewGuardedCache(c),
		ttl:           ttl,
		invalidatedAt: make(map[string]time.Time),
	}
}

func (cpr *CachedPlanRepo) cacheLayer() string {
	switch cpr.cache.(type) {
	case *cache.Redis:
		return "redis"
	default:
		return "inmemory"
	}
}

func (cpr *CachedPlanRepo) cacheKey(id string) string {
	return "plan:byid:" + id
}

func (cpr *CachedPlanRepo) listKey() string {
	return "plan:list:all"
}

// isStale returns true if the envelope was stored before the last invalidation of key.
func (cpr *CachedPlanRepo) isStale(key string, env cacheEnvelope) bool {
	cpr.invalidatedMu.RLock()
	t, ok := cpr.invalidatedAt[key]
	cpr.invalidatedMu.RUnlock()
	return ok && env.StoredAt.Before(t)
}

// readEnvelope attempts to load and unmarshal a cacheEnvelope for key.
// It returns (nil, false) on cache miss or error.
func (cpr *CachedPlanRepo) readEnvelope(ctx context.Context, key string) (*cacheEnvelope, bool) {
	if cpr.cache == nil {
		return nil, false
	}
	val, err := cpr.cache.Get(ctx, key)
	if err != nil || val == nil {
		return nil, false
	}
	var env cacheEnvelope
	if err := json.Unmarshal(val, &env); err != nil {
		return nil, false
	}
	return &env, true
}

// FindByID implements PlanRepository. It reads from cache first, falls back to backend
// and updates cache on a successful backend read.
func (cpr *CachedPlanRepo) FindByID(ctx context.Context, id string) (*PlanRow, error) {
	key := cpr.cacheKey(id)

	// Fast path: fresh cache hit
	if env, ok := cpr.readEnvelope(ctx, key); ok && !cpr.isStale(key, *env) {
		var pr PlanRow
		if err := json.Unmarshal(env.Data, &pr); err == nil {
			atomic.AddUint64(&cpr.hits, 1)
			cpr.recordCacheMetrics("get", "hit")
			return &pr, nil
		}
		cpr.recordCacheMetrics("get", "error")
		// Inner data corrupt; purge so GetOrLoad refreshes
		_ = cpr.cache.Delete(ctx, key)
	}

	// Stale path: cached but invalidated — purge so GetOrLoad loads fresh
	if env, ok := cpr.readEnvelope(ctx, key); ok && cpr.isStale(key, *env) {
		atomic.AddUint64(&cpr.stales, 1)
		cpr.recordCacheMetrics("get", "stale")
		_ = cpr.cache.Delete(ctx, key)
	}

	// Miss or stale-removed path: guarded load from backend
	atomic.AddUint64(&cpr.misses, 1)
	cpr.recordCacheMetrics("get", "miss")
	envelopeBytes, err := cpr.guard.GetOrLoad(ctx, key, cpr.ttl, func() ([]byte, error) {
		pr, err := cpr.backend.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(pr)
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
	var pr PlanRow
	if err := json.Unmarshal(env.Data, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// List returns all plans. It caches the full list under a single key.
func (cpr *CachedPlanRepo) List(ctx context.Context) ([]*PlanRow, error) {
	key := cpr.listKey()

	// Fast path: fresh cache hit
	if env, ok := cpr.readEnvelope(ctx, key); ok && !cpr.isStale(key, *env) {
		var out []*PlanRow
		if err := json.Unmarshal(env.Data, &out); err == nil {
			atomic.AddUint64(&cpr.hits, 1)
			cpr.recordCacheMetrics("get", "hit")
			return out, nil
		}
		cpr.recordCacheMetrics("get", "error")
		// Inner data corrupt; purge so GetOrLoad refreshes
		_ = cpr.cache.Delete(ctx, key)
	}

	// Stale path: cached but invalidated — purge so GetOrLoad loads fresh
	if env, ok := cpr.readEnvelope(ctx, key); ok && cpr.isStale(key, *env) {
		atomic.AddUint64(&cpr.stales, 1)
		cpr.recordCacheMetrics("get", "stale")
		_ = cpr.cache.Delete(ctx, key)
	}

	// Miss or stale-removed path: guarded load from backend
	atomic.AddUint64(&cpr.misses, 1)
	cpr.recordCacheMetrics("get", "miss")
	envelopeBytes, err := cpr.guard.GetOrLoad(ctx, key, cpr.ttl, func() ([]byte, error) {
		out, err := cpr.backend.List(ctx)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(out)
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
	var out []*PlanRow
	if err := json.Unmarshal(env.Data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete invalidates a cached plan entry and records the invalidation time.
func (cpr *CachedPlanRepo) Delete(ctx context.Context, id string) error {
	if cpr.cache == nil {
		return nil
	}
	key := cpr.cacheKey(id)
	listKey := cpr.listKey()

	_ = cpr.guard.Delete(ctx, key)
	_ = cpr.guard.Delete(ctx, listKey)
	cpr.recordCacheMetrics("delete", "hit")

	now := time.Now()
	cpr.invalidatedMu.Lock()
	cpr.invalidatedAt[key] = now
	cpr.invalidatedAt[listKey] = now
	cpr.invalidatedMu.Unlock()
	return nil
}

// Flush implements cache.Purgeable. It evicts all known cached entries from
// the underlying cache and marks them as invalidated so subsequent reads
// bypass stale data and reload from the backend.
func (cpr *CachedPlanRepo) Flush(ctx context.Context) (int, error) {
	cpr.invalidatedMu.Lock()
	count := len(cpr.invalidatedAt)
	now := time.Now()

	// Delete from underlying cache first; errors are non-fatal since the
	// stale-read detection will catch any remaining entries.
	if cpr.cache != nil {
		for k := range cpr.invalidatedAt {
			_ = cpr.cache.Delete(ctx, k)
		}
	}

	// Update invalidation timestamps after deletion to minimize the window
	// where a concurrent read could see a stale entry as fresh.
	for k := range cpr.invalidatedAt {
		cpr.invalidatedAt[k] = now
	}
	cpr.invalidatedMu.Unlock()
	cpr.recordCacheMetrics("flush", "hit")
	return count, nil
}

// ResetMetrics implements cache.Purgeable.
func (cpr *CachedPlanRepo) ResetMetrics() {
	atomic.StoreUint64(&cpr.hits, 0)
	atomic.StoreUint64(&cpr.misses, 0)
	atomic.StoreUint64(&cpr.stales, 0)
}

// Namespace implements cache.Purgeable.
func (cpr *CachedPlanRepo) Namespace() string {
	return "plans"
}

// Metrics returns hit/miss/stale counters for testing/monitoring.
func (cpr *CachedPlanRepo) Metrics() (hits uint64, misses uint64, stales uint64) {
	return atomic.LoadUint64(&cpr.hits),
		atomic.LoadUint64(&cpr.misses),
		atomic.LoadUint64(&cpr.stales)
}

// recordCacheMetrics records a cache operation to Prometheus.
func (cpr *CachedPlanRepo) recordCacheMetrics(op, result string) {
	metrics.CacheHitsTotal.WithLabelValues(cpr.cacheLayer(), op, result).Inc()
}
