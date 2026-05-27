# Read Caching Strategy

## Overview

This document describes the caching strategy for high-read endpoints (plans and subscriptions) with explicit invalidation, stale-read detection, and cache stampede protection.

## Goals

- Reduce database load for frequent reads of plan and subscription metadata.
- Improve read latency for plan list, plan detail, and subscription detail endpoints.
- Prevent stale reads from affecting billing decisions.
- Provide safe invalidation on mutations.

## Architecture

### Cache Abstraction

The `internal/cache.Cache` interface provides a minimal contract:

- `Get(ctx, key) ([]byte, error)` — loads value for key.
- `Set(ctx, key, value, ttl) error` — stores value with TTL.
- `Delete(ctx, key) error` — removes a key.

### In-Memory Backend

`cache.NewInMemory()` provides a thread-safe in-memory implementation with TTL expiry, suitable for local development and tests.

### GuardedCache (Stampede Protection)

`cache.NewGuardedCache(c Cache)` wraps any `Cache` with per-key stampede protection using `sync.Map` of `*sync.Mutex`.

When a cache miss occurs for a hot key, only one goroutine executes the database loader. Others wait on the per-key mutex, then read the freshly cached value.

```go
guard := cache.NewGuardedCache(memCache)
data, err := guard.GetOrLoad(ctx, key, ttl, func() ([]byte, error) {
    // Only one goroutine executes this
    return queryDatabase()
})
```

## Repository Decorators

### CachedPlanRepo

Wraps `PlanRepository` with read-through caching.

**Cache keys:**
- `plan:byid:<id>` — individual plan rows.
- `plan:list:all` — full plan list.

**Methods cached:**
- `FindByID(ctx, id)` → key `plan:byid:<id>`
- `List(ctx)` → key `plan:list:all`

**Invalidation:** `Delete(ctx, id)` removes both the per-id key and the list key, recording invalidation timestamps to detect stale reads.

### CachedSubscriptionRepo

Wraps `SubscriptionRepository` with read-through caching.

**Cache keys:**
- `sub:byid:<id>` — subscription by ID.
- `sub:byidandtenant:<id>:<tenantID>` — tenant-scoped subscription lookup.

**Methods cached:**
- `FindByID(ctx, id)` → key `sub:byid:<id>`
- `FindByIDAndTenant(ctx, id, tenantID)` → key `sub:byidandtenant:<id>:<tenantID>`

**Invalidation:** `Delete(ctx, id, tenantID)` removes both keys and records invalidation timestamps.

## Stale-Read Detection

### The Problem

After calling `Delete(key)`, an in-flight request may still write stale data back to the cache (race condition). Subsequent reads would then serve outdated data.

### The Solution

Cached values are wrapped in a `cacheEnvelope`:

```go
type cacheEnvelope struct {
    Data     []byte    `json:"data"`
    StoredAt time.Time `json:"stored_at"`
}
```

When `Delete()` is called, it records the invalidation time:

```go
invalidatedAt[key] = time.Now()
```

On read, if `env.StoredAt < invalidatedAt[key]`, the entry is stale:
- Increment `stales` metric.
- Purge the stale entry.
- Refetch from backend via `GuardedCache.GetOrLoad`.

## Metrics

Each decorator exposes hit/miss/stale counters via `Metrics()`:

| Metric | Meaning |
|---|---|
| `hits` | Cache read returned fresh data. |
| `misses` | Cache empty or expired; backend queried. |
| `stales` | Stale entry detected after invalidation; backend re-queried. |

## Security Considerations

### Tenant Isolation

`FindByIDAndTenant` uses tenant-scoped cache keys (`sub:byidandtenant:<id>:<tenantID>`). This prevents cross-tenant cache leakage. The raw `FindByID` key (`sub:byid:<id>`) should only be used when tenant checks are performed upstream.

### Privilege Bypass Prevention

Caching occurs **after** authorization checks in the service/handler layer. The cache stores data that has already been validated for the caller's permissions. Never cache pre-authorization responses.

### No PII in Cache

Plan and subscription cache entries contain metadata (amount, currency, interval, status) but no personally identifiable information. If PII fields are added in the future, they must be excluded from cache serialization or encrypted at rest.

### Billing Safety

Stale-read detection ensures that after a plan price or subscription status mutation, cached values predating the invalidation are detected and refreshed. This prevents incorrect charges based on stale pricing.

## Failure Modes

| Scenario | Behavior |
|---|---|
| Cache outage | Falls back to backend; no data loss. |
| Cache stampede | `GuardedCache` serializes loads per key; only 1 backend query. |
| Stale read after invalidation | Detected via timestamp comparison; refetched automatically. |
| Concurrent invalidation + read | Per-key mutex ensures atomicity; stale entries are purged. |

## TTL Recommendations

| Environment | TTL |
|---|---|
| Local / Test | 1–5 minutes |
| Production (plans) | 60–300 seconds |
| Production (subscriptions) | 30–120 seconds (more volatile) |

## Running Tests

```bash
go test ./internal/repository -run "Cached" -v
```

Tests cover:
- Cache hit/miss behavior and TTL expiry
- Stale-read detection and automatic refresh
- Fallback when cache operations error
- Concurrent invalidation under load
- Cache stampede protection (single backend query per key)

## Files

| File | Purpose |
|---|---|
| `internal/cache/cache.go` | Cache interface, InMemory backend, GuardedCache |
| `internal/repository/cached_plan_repo.go` | Plan cache decorator |
| `internal/repository/cached_subscription_repo.go` | Subscription cache decorator |
| `internal/repository/cached_plan_repo_test.go` | Plan cache tests |
| `internal/repository/cached_subscription_repo_test.go` | Subscription cache tests |