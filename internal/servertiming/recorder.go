// Package servertiming provides a context-scoped recorder for tracking
// per-request latency breakdowns (DB, cache, outbox). It has no
// dependency on HTTP frameworks so lower-level packages (cache,
// repository, db) can record timings without importing middleware.
package servertiming

import (
	"context"
	"sync"
	"time"
)

// recorderContextKey is the context key for the timing recorder.
type recorderContextKey struct{}

// Recorder tracks latencies for DB, cache, and outbox.
type Recorder struct {
	mu          sync.Mutex
	dbTotal     time.Duration
	cacheTotal  time.Duration
	outboxTotal time.Duration
}

// RecordDB adds to the total DB duration.
func (r *Recorder) RecordDB(d time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dbTotal += d
}

// RecordCache adds to the total Cache duration.
func (r *Recorder) RecordCache(d time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cacheTotal += d
}

// RecordOutbox adds to the total Outbox duration.
func (r *Recorder) RecordOutbox(d time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outboxTotal += d
}

// Totals returns the accumulated db, cache, and outbox durations.
func (r *Recorder) Totals() (db, cache, outbox time.Duration) {
	if r == nil {
		return 0, 0, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dbTotal, r.cacheTotal, r.outboxTotal
}

// WithContext returns a new context with rec attached.
func WithContext(ctx context.Context, rec *Recorder) context.Context {
	return context.WithValue(ctx, recorderContextKey{}, rec)
}

// FromContext extracts the Recorder from the given context.
func FromContext(ctx context.Context) *Recorder {
	if ctx == nil {
		return nil
	}
	val := ctx.Value(recorderContextKey{})
	if rec, ok := val.(*Recorder); ok {
		return rec
	}
	return nil
}
