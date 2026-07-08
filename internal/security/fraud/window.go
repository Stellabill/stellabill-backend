package fraud

import (
	"time"
)

// Clock abstracts time retrieval so callers (and tests) can control the
// notion of "now". Implementations must always return UTC instants so that
// window math is unaffected by the process' local timezone.
type Clock interface {
	Now() time.Time
}

// systemClock is the default Clock backed by the wall clock, normalized to UTC.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// slidingWindow is a fixed-duration sliding window counter implemented as a
// ring of sub-window buckets. Splitting the window into buckets keeps the
// count approximately accurate as time advances (older buckets fall out of
// range) without re-scanning an unbounded event list.
//
// The zero value is not usable; construct via newSlidingWindow.
type slidingWindow struct {
	window     time.Duration // total span the window observes
	bucketSpan time.Duration // duration represented by a single bucket
	buckets    []int64       // per-bucket event counts (ring buffer)
	bucketTime []time.Time   // start instant of the data currently in each bucket
}

// newSlidingWindow builds a sliding window covering window with the requested
// number of buckets. buckets must be >= 1; window must be > 0. Higher bucket
// counts yield finer-grained roll-over at the cost of memory.
func newSlidingWindow(window time.Duration, buckets int) *slidingWindow {
	if buckets < 1 {
		buckets = 1
	}
	if window <= 0 {
		window = time.Second
	}
	return &slidingWindow{
		window:     window,
		bucketSpan: window / time.Duration(buckets),
		buckets:    make([]int64, buckets),
		bucketTime: make([]time.Time, buckets),
	}
}

// bucketIndex maps an instant to its slot in the ring buffer.
func (w *slidingWindow) bucketIndex(t time.Time) int {
	// UnixNano keeps the mapping monotonic and timezone-independent.
	idx := (t.UnixNano() / int64(w.bucketSpan)) % int64(len(w.buckets))
	if idx < 0 {
		idx += int64(len(w.buckets))
	}
	return int(idx)
}

// bucketStart returns the start instant of the bucket that contains t.
func (w *slidingWindow) bucketStart(t time.Time) time.Time {
	return time.Unix(0, (t.UnixNano()/int64(w.bucketSpan))*int64(w.bucketSpan)).UTC()
}

// roll lazily resets any bucket whose stored data is older than the current
// bucket window. This is what makes counts "expire" as time advances and is
// the core of correct window roll-over.
func (w *slidingWindow) roll(now time.Time) {
	for i := range w.buckets {
		// A bucket is stale if it has never been written, or if the data it
		// holds belongs to a bucket-span that is no longer within the window.
		if w.bucketTime[i].IsZero() {
			continue
		}
		if now.Sub(w.bucketTime[i]) >= w.window {
			w.buckets[i] = 0
			w.bucketTime[i] = time.Time{}
		}
	}
}

// add records n events occurring at instant now and returns the resulting
// total count within the window.
func (w *slidingWindow) add(now time.Time, n int64) int64 {
	now = now.UTC()
	w.roll(now)

	idx := w.bucketIndex(now)
	start := w.bucketStart(now)

	// If the bucket currently holds data from a different bucket-span (it was
	// reused by the ring), reset it before accumulating into it.
	if !w.bucketTime[idx].Equal(start) {
		w.buckets[idx] = 0
		w.bucketTime[idx] = start
	}
	w.buckets[idx] += n

	return w.sum(now)
}

// count returns the current total within the window without recording an event.
func (w *slidingWindow) count(now time.Time) int64 {
	now = now.UTC()
	w.roll(now)
	return w.sum(now)
}

// sum totals every bucket still within the window relative to now.
func (w *slidingWindow) sum(now time.Time) int64 {
	var total int64
	for i := range w.buckets {
		if w.bucketTime[i].IsZero() {
			continue
		}
		if now.Sub(w.bucketTime[i]) < w.window {
			total += w.buckets[i]
		}
	}
	return total
}

// empty reports whether the window holds no live events as of now. Used by the
// collector to decide when an idle tenant entry may be evicted.
func (w *slidingWindow) empty(now time.Time) bool {
	return w.count(now) == 0
}
