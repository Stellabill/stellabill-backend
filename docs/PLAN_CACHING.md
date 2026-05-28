# Read Caching with Explicit Invalidation

This document describes the caching strategy for high-read endpoints (plans and subscriptions) with safe invalidation, stale-read detection, and cache stampede protection.

## Goals

- Reduce DB load for frequent reads of plan and subscription metadata.
- Improve read latency for plan list, plan detail, and subscription detail endpoints.
- Prevent stale reads from affecting billing decisions.
- Provide configurable adapters (in-memory for local/dev, Redis for production).

## Architecture

### Cache Abstraction

The `cache.Cache` interface is a minimal key-value contract:

```go
type Cache interface {
    Get(ctx context.Context, key string) ([]byte, error)
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
}
```

`cache.InMemory` provides a thread-safe in-memory implementation with TTL expiry. For production, a Redis-backed adapter can implement the same interface.

### Stampede Protection

`cache.GuardedCache` wraps any `Cache` with per-key singleflight protection:

- On cache miss, a per-key mutex is acquired via `sync.Map`.
- Only the first goroutine executes the database loader.
- Subsequent goroutines wait, then read the freshly cached value.
- A double-check after acquiring the lock prevents redundant loads if another goroutine won the race.

```go
guard := cache.NewGuardedCache(redisAdapter)
data, err := guard.GetOrLoad(ctx, key, ttl, func() ([]byte, error) {
    // Only ONE goroutine executes this per key
    return queryDatabase()
})
```

## Plan Caching

### Cache Keys

| Method | Cache Key |
|--------|-----------|
| `FindByID(id)` | `plan:byid:<id>` |
| `List()` | `plan:list:all` |

### TTL

Configurable per `CachedPlanRepo` instance. Default in tests is small; in production choose 60s–300s.

### Stale-Read Detection

Each cached value is wrapped in a `cacheEnvelope` containing the serialized data and a `StoredAt` timestamp:

```go
type cacheEnvelope struct {
    Data     []byte
    StoredAt time.Time
}
```

When a plan is mutated, `Delete(id)` is called. This:
1. Removes the key from the cache.
2. Records the invalidation time in `invalidatedAt[key]`.

If a concurrent in-flight request writes stale data back to the cache after deletion, the next reader detects