# Plan Read Caching

This document describes the caching strategy implemented for plan read paths (plan detail and plan list).

Goals

- Reduce DB load for frequent reads of plan metadata.
- Improve read latency for plan list and plan detail endpoints.
- Provide configurable adapters (in-memory for local/dev, Redis for production).

Key points

- A read-through cache decorator `repository.NewCachedPlanRepo(backend, cache, ttl)` wraps any `PlanRepository`.
- Methods cached:
  - `FindByID(ctx, id)` caches individual plan rows under `plan:byid:<id>`.
  - `List(ctx)` caches the full plan list under `plan:list:all`.
- TTL: configurable per CachedPlanRepo instance (passed at construction). Default in tests is small; in production choose a value like 60s-300s.
- Invalidation: `CachedPlanRepo.Delete(ctx, id)` removes both the per-id key and the list key. Call this from any write/update path when a plan is modified or removed.
- Metrics: simple hit/miss counters exposed by `Metrics()` on the cached repo (uint64 hits, misses). These are easy to extend to Prometheus counters.

Security notes

- Plan data stored in cache may be considered non-sensitive metadata. Do not store personally identifiable information (PII) or secrets in the cache.
- When using Redis in production:
  - Use TLS for network connections where applicable.
  - Require AUTH with strong credentials and use least-privilege Redis users.
  - Configure Redis persistence and eviction policies according to traffic and memory needs.
  - Ensure Redis is not publicly accessible and is placed in a private network.

Testing notes

- The repository includes unit tests covering:
  - cache hit/miss behavior and TTL expiry
  - fallback when cache operations error
  - concurrent invalidation
- Run tests with: `go test ./...` (note: some repo tests may require environment configuration; the cache tests run without external dependencies).
