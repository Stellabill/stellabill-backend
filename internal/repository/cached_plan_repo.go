package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"stellarbill-backend/internal/cache"
)

type cacheEnvelope struct {
	Data     []byte    `json:"data"`
	StoredAt time.Time `json:"stored_at"`
}

type inflightLoad struct {
	wg  sync.WaitGroup
	row interface{}
	err error
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
	inflight      sync.Map // map[string]*inflightLoad
}

// NewCachedPlanRepo constructs a CachedPlanRepo.
func NewCachedPlanRepo(backend PlanRepository, c cache.Cache, ttl time.Duration) *CachedPlanRepo {
	return &CachedPlanRepo{backend: backend, cache: c, ttl: ttl}
}

func (cpr *CachedPlanRepo) listKey() string            { return "plan:list:all" }
func (cpr *CachedPlanRepo) cacheKey(id string) string  { return "plan:byid:" + id }

func (cpr *CachedPlanRepo) getCachedPlan(ctx context.Context, key string) (*PlanRow, bool, error) {
	if cpr.cache == nil {
		return nil, false, nil
	}
	val, err := cpr.cache.Get(ctx, key)
	if err != nil || val == nil {
		return nil, false, nil
	}
	var env cacheEnvelope
	if err := json.Unmarshal(val, &env); err != nil {
		return nil, true, err
	}
	if inv, ok := cpr.invalidatedAt.Load(key); ok {
		if invt, ok2 := inv.(time.Time); ok2 && env.StoredAt.Before(invt) {
			atomic.AddUint64(&cpr.stales, 1)
			_ = cpr.cache.Delete(ctx, key)
			return nil, false, nil
		}
	}
	var pr PlanRow
	if err := json.Unmarshal(env.Data, &pr); err != nil {
		return nil, false, err
	}
	atomic.AddUint64(&cpr.hits, 1)
	return &pr, true, nil
}

func (cpr *CachedPlanRepo) FindByID(ctx context.Context, id string) (*PlanRow, error) {
	key := cpr.cacheKey(id)
	if pr, ok, err := cpr.getCachedPlan(ctx, key); ok {
		return pr, err
	}

	load := &inflightLoad{}
	load.wg.Add(1)
	actual, loaded := cpr.inflight.LoadOrStore(key, load)
	if loaded {
		inflight := actual.(*inflightLoad)
		inflight.wg.Wait()
		if inflight.err == nil {
			atomic.AddUint64(&cpr.hits, 1)
		}
		if inflight.row == nil {
			return nil, inflight.err
		}
		return inflight.row.(*PlanRow), inflight.err
	}
	defer func() {
		load.wg.Done()
		cpr.inflight.Delete(key)
	}()

	atomic.AddUint64(&cpr.misses, 1)
	pr, err := cpr.backend.FindByID(ctx, id)
	load.row = pr
	load.err = err
	if err != nil {
		return nil, err
	}
	if cpr.cache != nil {
		if prBytes, marshalErr := json.Marshal(pr); marshalErr == nil {
			env := cacheEnvelope{Data: prBytes, StoredAt: time.Now()}
			if envBytes, marshalErr := json.Marshal(env); marshalErr == nil {
				_ = cpr.cache.Set(ctx, key, envBytes, cpr.ttl)
			}
		}
	}
	return pr, nil
}

func (cpr *CachedPlanRepo) List(ctx context.Context) ([]*PlanRow, error) {
	key := cpr.listKey()

	if cpr.cache != nil {
		if val, err := cpr.cache.Get(ctx, key); err == nil && val != nil {
			var env cacheEnvelope
			if err := json.Unmarshal(val, &env); err == nil {
				stale := false
				if inv, ok := cpr.invalidatedAt.Load(key); ok {
					if invt, ok2 := inv.(time.Time); ok2 && env.StoredAt.Before(invt) {
						stale = true
					}
				}
				if stale {
					atomic.AddUint64(&cpr.stales, 1)
					_ = cpr.cache.Delete(ctx, key)
				} else {
					var out []*PlanRow
					if unmarshalErr := json.Unmarshal(env.Data, &out); unmarshalErr == nil {
						atomic.AddUint64(&cpr.hits, 1)
						return out, nil
					} else {
						return nil, fmt.Errorf("corrupted cache envelope: %w", unmarshalErr)
					}
				}
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
			env := cacheEnvelope{Data: b, StoredAt: time.Now()}
			if eb, err := json.Marshal(env); err == nil {
				_ = cpr.cache.Set(ctx, key, eb, cpr.ttl)
			}
		}
	}
	return out, nil
}

func (cpr *CachedPlanRepo) Delete(ctx context.Context, id string) error {
	if cpr.cache == nil {
		return nil
	}
	key := cpr.cacheKey(id)
	now := time.Now()
	cpr.invalidatedAt.Store(key, now)
	cpr.invalidatedAt.Store(cpr.listKey(), now)
	_ = cpr.cache.Delete(ctx, key)
	_ = cpr.cache.Delete(ctx, cpr.listKey())
	return nil
}

func (cpr *CachedPlanRepo) Metrics() (uint64, uint64, uint64) {
	return atomic.LoadUint64(&cpr.hits), atomic.LoadUint64(&cpr.misses), atomic.LoadUint64(&cpr.stales)
}

func (cpr *CachedPlanRepo) Flush(ctx context.Context) (int, error) {
	if cpr.cache == nil {
		return 0, nil
	}
	if f, ok := cpr.cache.(cache.Flushable); ok {
		return f.Flush(ctx)
	}
	_ = cpr.cache.Delete(ctx, cpr.listKey())
	return 0, nil
}

// Namespace returns the human-readable label for this cache namespace.
func (cpr *CachedPlanRepo) Namespace() string { return "plans" }

// ResetMetrics zeroes the hit/miss/stale counters.
func (cpr *CachedPlanRepo) ResetMetrics() {
	atomic.StoreUint64(&cpr.hits, 0)
	atomic.StoreUint64(&cpr.misses, 0)
	atomic.StoreUint64(&cpr.stales, 0)
}
