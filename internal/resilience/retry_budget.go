// Package resilience provides circuit-breaking and retry-budget primitives
// that protect downstream services from cascading failures and
// thundering-herd storms.
package resilience

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// DefaultRetryBudget returns a RetryBudget with sensible production defaults:
// a 10% retry ratio over a 30-second sliding window with 10 buckets.
func DefaultRetryBudget() RetryBudget {
	return RetryBudget{
		Ratio:      0.10,
		WindowSize: 30 * time.Second,
		Buckets:    10,
	}
}

// RetryBudget enforces that retries may not exceed a configurable fraction of
// successful requests over a sliding time window. Once the budget is exhausted,
// subsequent retries are denied (fail fast) until the window slides enough that
// the ratio falls back below the threshold.
//
// The implementation uses a ring of time buckets. Each bucket tracks successes
// and retries that fell into its time slice. When the budget is consulted the
// bucket corresponding to the current time is used (or reset if the window has
// moved).
//
// RetryBudget is safe for concurrent use.
type RetryBudget struct {
	// Ratio is the maximum allowed ratio of retries to successful requests
	// over the sliding window. A value of 0.10 means retries may not exceed
	// 10% of successful requests. Must be in (0, 1].
	Ratio float64
	// WindowSize is the duration of the sliding window.
	WindowSize time.Duration
	// Buckets is the number of time buckets the window is divided into.
	// More buckets give smoother sliding but use more memory.
	// Must be at least 1.
	Buckets int

	once sync.Once
	mu   sync.Mutex

	// ring of buckets, indexed by bucketIdx()
	bucketSuccess []int64
	bucketRetries []int64
	lastTick      time.Time // last time a bucket index was computed (approx "now")
}

// bucketIndex returns the bucket index for the current clock time. The result
// depends on lastTick so that repeated calls within the same time slice return
// the same bucket.
func (b *RetryBudget) bucketIndex(now time.Time) int {
	bucketDur := b.WindowSize / time.Duration(b.Buckets)
	elapsed := now.Sub(b.lastTick)
	// If the elapsed time exceeds the window, we've slid past every bucket
	// and can reset everything.
	if elapsed >= b.WindowSize {
		for i := range b.bucketSuccess {
			b.bucketSuccess[i] = 0
			b.bucketRetries[i] = 0
		}
		b.lastTick = now
		return 0
	}
	// How many full buckets have we advanced?
	advance := int(elapsed / bucketDur)
	if advance > 0 {
		// Zero out the buckets we're skipping over.
		for i := 0; i < advance && i < b.Buckets; i++ {
			idx := (i + 1) % b.Buckets // skip current bucket
			b.bucketSuccess[idx] = 0
			b.bucketRetries[idx] = 0
		}
		b.lastTick = now
	}
	return advance % b.Buckets
}

// initLazy initializes the ring buffers on first use so that callers can
// construct RetryBudget literals or use DefaultRetryBudget() without
// additional ceremony.
func (b *RetryBudget) initLazy() {
	b.once.Do(func() {
		if b.Buckets <= 0 {
			b.Buckets = 10
		}
		if b.WindowSize <= 0 {
			b.WindowSize = 30 * time.Second
		}
		if b.Ratio <= 0 || b.Ratio > 1 {
			b.Ratio = 0.10
		}
		b.bucketSuccess = make([]int64, b.Buckets)
		b.bucketRetries = make([]int64, b.Buckets)
		b.lastTick = time.Now()
	})
}

// RecordSuccess records a successful request in the current time bucket.
func (b *RetryBudget) RecordSuccess() {
	b.initLazy()
	b.mu.Lock()
	defer b.mu.Unlock()

	idx := b.bucketIndex(time.Now())
	b.bucketSuccess[idx]++
}

// AllowRetry reports whether a retry is permitted under the current budget.
// It does NOT record the retry; call RecordRetry only when the retry is
// actually attempted.
func (b *RetryBudget) AllowRetry() bool {
	b.initLazy()
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	_ = b.bucketIndex(now) // slide window

	var totalSuccess, totalRetries int64
	for i := 0; i < b.Buckets; i++ {
		totalSuccess += b.bucketSuccess[i]
		totalRetries += b.bucketRetries[i]
	}

	// If there are no successful requests, retries are not allowed (the
	// downstream is already unhealthy).
	if totalSuccess == 0 {
		return false
	}

	ratio := float64(totalRetries) / float64(totalSuccess)
	return ratio <= b.Ratio
}

// RecordRetry records a retry in the current time bucket. Call this only after
// AllowRetry has returned true.
func (b *RetryBudget) RecordRetry() {
	b.initLazy()
	b.mu.Lock()
	defer b.mu.Unlock()

	idx := b.bucketIndex(time.Now())
	b.bucketRetries[idx]++
}

// Available returns the fraction of the retry budget that is still available.
// A value of 0.5 means half the budget has been consumed; 0.0 means the budget
// is exhausted; 1.0 means no retries have been recorded.
func (b *RetryBudget) Available() float64 {
	b.initLazy()
	b.mu.Lock()
	defer b.mu.Unlock()

	_ = b.bucketIndex(time.Now())

	var totalSuccess, totalRetries int64
	for i := 0; i < b.Buckets; i++ {
		totalSuccess += b.bucketSuccess[i]
		totalRetries += b.bucketRetries[i]
	}

	if totalSuccess == 0 {
		return 0
	}
	currentRatio := float64(totalRetries) / float64(totalSuccess)
	available := (b.Ratio - currentRatio) / b.Ratio
	if available < 0 {
		return 0
	}
	return available
}

// Snapshot returns the current counts for observability.
type RetryBudgetSnapshot struct {
	Ratio        float64
	SuccessTotal int64
	RetriesTotal int64
	CurrentRatio float64
}

// Snapshot returns a point-in-time view of the budget counters.
func (b *RetryBudget) Snapshot() RetryBudgetSnapshot {
	b.initLazy()
	b.mu.Lock()
	defer b.mu.Unlock()

	_ = b.bucketIndex(time.Now())

	var totalSuccess, totalRetries int64
	for i := 0; i < b.Buckets; i++ {
		totalSuccess += b.bucketSuccess[i]
		totalRetries += b.bucketRetries[i]
	}

	var currentRatio float64
	if totalSuccess > 0 {
		currentRatio = float64(totalRetries) / float64(totalSuccess)
	}

	return RetryBudgetSnapshot{
		Ratio:        b.Ratio,
		SuccessTotal: totalSuccess,
		RetriesTotal: totalRetries,
		CurrentRatio: math.Round(currentRatio*1000) / 1000,
	}
}

// String implements fmt.Stringer for logging.
func (s RetryBudgetSnapshot) String() string {
	return fmt.Sprintf("ratio=%.2f success=%d retries=%d current_ratio=%.3f",
		s.Ratio, s.SuccessTotal, s.RetriesTotal, s.CurrentRatio)
}
