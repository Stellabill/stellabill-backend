// Package security provides primitives for admin authentication hardening,
// including exponential lockout tracking for failed login attempts.
//
// # Lockout reset procedure
//
// A lockout is automatically reset when a successful login occurs for the
// same source+account pair.  Operators can also manually reset a lockout
// by calling LockoutTracker.Reset(source, account) — this clears the
// consecutive-failure counter and removes the rate-limit.  Since entries
// are in-memory (per-replica), a server restart also clears all state.
//
// Clock skew across replicas is handled by using UTC-normalized timestamps
// throughout; each replica independently tracks its own lockout state, so
// a few seconds of drift between machines will not cause double-counting
// of failures.
package security

import (
	"fmt"
	"sync"
	"time"

	"stellarbill-backend/internal/timeutil"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// DefaultBaseLockout is the initial lockout duration after one failure.
	DefaultBaseLockout = 1 * time.Second

	// DefaultMaxLockout is the ceiling for exponential backoff.
	DefaultMaxLockout = 15 * time.Minute

	// defaultCleanupEvery controls how often expired entries are reaped.
	defaultCleanupEvery = time.Minute
)

// AdminLoginLockoutsTotal counts every lockout-triggering event, labelled by
// source IP and account name.  Use this metric in Prometheus alerting rules
// to detect brute-force attacks against the admin login endpoint.
var AdminLoginLockoutsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "admin_login_lockouts_total",
		Help: "Total number of admin login lockouts by source IP and account",
	},
	[]string{"source", "account"},
)

// lockoutEntry holds the per-key state for one source+account combination.
type lockoutEntry struct {
	attempts   int
	lockedAt   time.Time
	lockoutFor time.Duration
}

// LockoutTracker provides per-source, per-account exponential lockout with
// thread-safe concurrent access.  A zero-value LockoutTracker is not usable;
// construct one with NewLockoutTracker.
type LockoutTracker struct {
	mu           sync.RWMutex
	entries      map[string]*lockoutEntry
	clock        timeutil.Clock
	baseLockout  time.Duration
	maxLockout   time.Duration
	lastCleanup  time.Time
	cleanupEvery time.Duration
}

// LockoutOption configures a LockoutTracker at construction time.
type LockoutOption func(*LockoutTracker)

// WithClock replaces the default system clock with c, enabling deterministic
// time control in tests.
func WithClock(c timeutil.Clock) LockoutOption {
	return func(lt *LockoutTracker) {
		lt.clock = c
	}
}

// WithBaseLockout overrides the initial lockout duration (default 1s).
func WithBaseLockout(d time.Duration) LockoutOption {
	return func(lt *LockoutTracker) {
		lt.baseLockout = d
	}
}

// WithMaxLockout overrides the lockout ceiling (default 15 min).
func WithMaxLockout(d time.Duration) LockoutOption {
	return func(lt *LockoutTracker) {
		lt.maxLockout = d
	}
}

// NewLockoutTracker returns a ready-to-use LockoutTracker.  Each option is
// applied in order; the lastCleanup timestamp is derived from the final clock.
func NewLockoutTracker(opts ...LockoutOption) *LockoutTracker {
	lt := &LockoutTracker{
		entries:      make(map[string]*lockoutEntry),
		clock:        timeutil.SystemClock,
		baseLockout:  DefaultBaseLockout,
		maxLockout:   DefaultMaxLockout,
		cleanupEvery: defaultCleanupEvery,
	}
	for _, opt := range opts {
		opt(lt)
	}
	lt.lastCleanup = lt.clock.Now()
	return lt
}

// lockoutKey builds the composite map key from source and account.
func lockoutKey(source, account string) string {
	return fmt.Sprintf("%s|%s", source, account)
}

// RecordFailure increments the consecutive-failure counter for the given
// source+account pair and establishes (or extends) a lockout window whose
// duration doubles with each failure, capped at MaxLockout.  It also
// increments the AdminLoginLockoutsTotal counter.
//
// The lockout is keyed by source IP + account name, so failures from
// different IPs or different usernames are tracked independently.
func (lt *LockoutTracker) RecordFailure(source, account string) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	lt.cleanupLocked()

	k := lockoutKey(source, account)
	entry, exists := lt.entries[k]
	if !exists {
		entry = &lockoutEntry{}
		lt.entries[k] = entry
	}

	entry.attempts++

	duration := lt.baseLockout
	for i := 1; i < entry.attempts; i++ {
		duration *= 2
		if duration >= lt.maxLockout {
			duration = lt.maxLockout
			break
		}
	}

	entry.lockedAt = lt.clock.Now()
	entry.lockoutFor = duration

	AdminLoginLockoutsTotal.WithLabelValues(source, account).Inc()
}

// IsLocked reports whether the given source+account pair is currently within
// a lockout window.  Expired entries are lazily cleaned up on the write path.
func (lt *LockoutTracker) IsLocked(source, account string) bool {
	lt.mu.RLock()
	entry, exists := lt.entries[lockoutKey(source, account)]
	if !exists {
		lt.mu.RUnlock()
		return false
	}
	now := lt.clock.Now()
	expiry := entry.lockedAt.Add(entry.lockoutFor)
	lt.mu.RUnlock()

	if now.Before(expiry) {
		return true
	}

	lt.mu.Lock()
	defer lt.mu.Unlock()
	if entry, exists := lt.entries[lockoutKey(source, account)]; exists {
		if now.After(entry.lockedAt.Add(entry.lockoutFor)) {
			delete(lt.entries, lockoutKey(source, account))
		}
	}
	return false
}

// Reset clears any lockout state for the given source+account pair, allowing
// immediate login attempts.  This is called automatically after a successful
// authentication and may also be used by operators for manual unblocking.
func (lt *LockoutTracker) Reset(source, account string) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	key := lockoutKey(source, account)
	delete(lt.entries, key)
}

// LockoutRemaining returns the duration until the lockout expires for the
// given source+account pair, rounded down to the nearest second.  Returns
// zero when no lockout is active or the entry has already expired.
func (lt *LockoutTracker) LockoutRemaining(source, account string) time.Duration {
	lt.mu.RLock()
	entry, exists := lt.entries[lockoutKey(source, account)]
	if !exists {
		lt.mu.RUnlock()
		return 0
	}
	now := lt.clock.Now()
	expiry := entry.lockedAt.Add(entry.lockoutFor)
	lt.mu.RUnlock()

	if now.Before(expiry) {
		return expiry.Sub(now).Truncate(time.Second)
	}
	return 0
}

// Attempts returns the consecutive-failure count for the given source+account
// pair.  Returns 0 when no prior failures have been recorded.
func (lt *LockoutTracker) Attempts(source, account string) int {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	entry, exists := lt.entries[lockoutKey(source, account)]
	if !exists {
		return 0
	}
	return entry.attempts
}

// cleanupLocked removes entries whose lockout window has expired.  It is
// called from RecordFailure so that stale entries do not accumulate
// indefinitely, but the sweep rate is throttled by cleanupEvery.
func (lt *LockoutTracker) cleanupLocked() {
	now := lt.clock.Now()
	if now.Sub(lt.lastCleanup) < lt.cleanupEvery {
		return
	}
	lt.lastCleanup = now
	for k, entry := range lt.entries {
		if now.After(entry.lockedAt.Add(entry.lockoutFor)) {
			delete(lt.entries, k)
		}
	}
}
