package fraud

import (
	"testing"
	"time"
)

func TestSlidingWindow_AddAndCount(t *testing.T) {
	w := newSlidingWindow(time.Minute, 6)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if got := w.add(base, 1); got != 1 {
		t.Fatalf("first add = %d, want 1", got)
	}
	if got := w.add(base.Add(time.Second), 2); got != 3 {
		t.Fatalf("after add(2) = %d, want 3", got)
	}
	if got := w.count(base.Add(2 * time.Second)); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}
}

func TestSlidingWindow_RollOver(t *testing.T) {
	w := newSlidingWindow(time.Minute, 6)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	w.add(base, 5)
	if got := w.count(base); got != 5 {
		t.Fatalf("count = %d, want 5", got)
	}
	// Advance just under the window: still counted.
	if got := w.count(base.Add(59 * time.Second)); got != 5 {
		t.Fatalf("count just under window = %d, want 5", got)
	}
	// Advance past the window: rolled out.
	if got := w.count(base.Add(time.Minute + time.Second)); got != 0 {
		t.Fatalf("count past window = %d, want 0", got)
	}
}

func TestSlidingWindow_PartialRollOver(t *testing.T) {
	w := newSlidingWindow(time.Minute, 6) // 10s buckets
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	w.add(base, 1)                     // bucket at t=0
	w.add(base.Add(30*time.Second), 1) // bucket at t=30s
	if got := w.count(base.Add(30 * time.Second)); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	// At t=65s the t=0 event has expired but the t=30s event remains.
	if got := w.count(base.Add(65 * time.Second)); got != 1 {
		t.Fatalf("partial roll count = %d, want 1", got)
	}
}

func TestSlidingWindow_RingReuse(t *testing.T) {
	// With a single bucket, a later add in a new bucket-span must reset the
	// stale value rather than accumulate onto it.
	w := newSlidingWindow(2*time.Second, 1)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	w.add(base, 10)
	if got := w.add(base.Add(10*time.Second), 1); got != 1 {
		t.Fatalf("ring reuse count = %d, want 1", got)
	}
}

func TestSlidingWindow_Empty(t *testing.T) {
	w := newSlidingWindow(time.Minute, 6)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !w.empty(base) {
		t.Fatal("new window should be empty")
	}
	w.add(base, 1)
	if w.empty(base) {
		t.Fatal("window with event should not be empty")
	}
	if !w.empty(base.Add(2 * time.Minute)) {
		t.Fatal("window should be empty after roll-over")
	}
}

func TestSlidingWindow_Defaults(t *testing.T) {
	w := newSlidingWindow(0, 0) // both invalid -> defaults
	if w.window != time.Second {
		t.Fatalf("window = %v, want 1s default", w.window)
	}
	if len(w.buckets) != 1 {
		t.Fatalf("buckets = %d, want 1 default", len(w.buckets))
	}
}

func TestSlidingWindow_NegativeUnixNanoIndex(t *testing.T) {
	// Instants before the Unix epoch yield negative UnixNano; bucketIndex must
	// still return a valid non-negative slot.
	w := newSlidingWindow(time.Minute, 6)
	pre := time.Date(1960, 1, 1, 0, 0, 0, 0, time.UTC)
	idx := w.bucketIndex(pre)
	if idx < 0 || idx >= len(w.buckets) {
		t.Fatalf("bucketIndex = %d, out of range", idx)
	}
	if got := w.add(pre, 1); got != 1 {
		t.Fatalf("add pre-epoch = %d, want 1", got)
	}
}

func TestSlidingWindow_NegativeIndexBranch(t *testing.T) {
	// Use a 1ns bucket span so consecutive pre-epoch nanoseconds map to
	// distinct (and negative) raw indices, forcing the wrap-around correction.
	w := newSlidingWindow(3*time.Nanosecond, 3)
	for off := int64(1); off <= 6; off++ {
		pre := time.Unix(0, -off).UTC()
		idx := w.bucketIndex(pre)
		if idx < 0 || idx >= len(w.buckets) {
			t.Fatalf("bucketIndex(%d) = %d, out of range", -off, idx)
		}
	}
}

func TestSystemClock(t *testing.T) {
	now := systemClock{}.Now()
	if now.Location() != time.UTC {
		t.Fatalf("system clock not UTC: %v", now.Location())
	}
}
