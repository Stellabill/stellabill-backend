package timeutil

import (
	"sync"
	"time"
)

// Clock abstracts wall-clock access so time-sensitive code (job schedulers,
// TTL expiry, archival cutoffs) can be driven deterministically in tests
// instead of depending on the real system clock.
type Clock interface {
	// Now returns the clock's current time, normalized to UTC.
	Now() time.Time
}

// realClock is the production Clock, backed by NowUTC.
type realClock struct{}

func (realClock) Now() time.Time { return NowUTC() }

// SystemClock is the default production Clock. Services should use this
// unless a Clock is explicitly injected (e.g. via a constructor or SetClock)
// for testing.
var SystemClock Clock = realClock{}

// FakeClock is a manually-controlled Clock for deterministic tests. It is
// safe for concurrent use. The zero value is not usable; construct one with
// NewFakeClock.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock returns a FakeClock fixed at start. start is normalized to UTC,
// matching the contract of Clock.Now.
func NewFakeClock(start time.Time) *FakeClock {
	return &FakeClock{now: NormalizeUTC(start)}
}

// Now returns the clock's current fixed time.
func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Set moves the clock to t (normalized to UTC).
func (f *FakeClock) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = NormalizeUTC(t)
}

// Advance moves the clock forward by d. A negative d moves it backward. This
// operates on the absolute instant (like time.Time.Add), so it is safe to use
// across daylight-saving-time transitions without introducing skew.
func (f *FakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
