package cache

import "context"

// Purgeable is implemented by cache-decorating repositories that support
// administrative cache invalidation via POST /api/admin/purge.
//
// Each implementation is responsible for its own concurrency safety.
// Flush must be idempotent: calling it on an already-empty cache returns 0
// keys evicted and no error.
type Purgeable interface {
	// Flush evicts all cached entries for this namespace and returns the
	// number of keys removed. Errors are reported per-namespace so that a
	// single failure does not prevent other namespaces from being flushed.
	Flush(ctx context.Context) (keysEvicted int, err error)

	// ResetMetrics zeroes any hit/miss counters tracked by the repository.
	ResetMetrics()

	// Namespace returns a stable, human-readable label used in purge
	// response summaries (e.g. "plans", "subscriptions").
	Namespace() string
}
