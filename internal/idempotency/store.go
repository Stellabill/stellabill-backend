// Package idempotency provides middleware and storage for idempotency key support.
// It prevents duplicate processing of mutating requests by caching responses
// keyed on the Idempotency-Key header value, scoped per authenticated caller
// to prevent cross-user key reuse from leaking data.
package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"stellarbill-backend/internal/timeutil"
)

const DefaultTTL = 24 * time.Hour

// Entry holds a cached response for a given idempotency key.
type Entry struct {
	StatusCode  int
	Body        []byte
	PayloadHash string // SHA-256 of the original request body
	Method      string // HTTP method bound to the original request
	Path        string // Request path bound to the original request
	CreatedAt   time.Time
}

// Expired reports whether the entry has exceeded its TTL.
func (e *Entry) Expired(ttl time.Duration) bool {
	return timeutil.NowUTC().After(e.CreatedAt.Add(ttl))
}

// Store is a thread-safe in-memory idempotency store. Keys are namespaced by
// scope (typically derived from the authenticated caller) so two callers using
// the same Idempotency-Key value do not collide.
type Store struct {
	mu      sync.Mutex
	entries map[string]*Entry
	ttl     time.Duration
	// inflight tracks scoped keys currently being processed to handle
	// concurrent duplicates.
	inflight map[string]chan struct{}
	stop     chan struct{}
}

// NewStore creates a Store with the given TTL and starts a background cleanup goroutine.
func NewStore(ttl time.Duration) *Store {
	s := &Store{
		entries:  make(map[string]*Entry),
		inflight: make(map[string]chan struct{}),
		ttl:      ttl,
		stop:     make(chan struct{}),
	}
	go s.cleanup()
	return s
}

// Stop terminates the background cleanup goroutine. Safe to call multiple times.
func (s *Store) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.stop:
		// already stopped
	default:
		close(s.stop)
	}
}

// HashPayload returns a hex-encoded SHA-256 hash of the given bytes.
func HashPayload(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// scopedKey composes a storage key. The separator (|) is not legal in HTTP
// header tokens and we additionally length-prefix the scope to make
// "user-a" + "|" + "key" indistinguishable from "user" + "|" + "a|key" impossible.
func scopedKey(scope, key string) string {
	// Hashing both sides yields fixed-length, collision-resistant components
	// and removes any risk of malicious scope/key crafting causing a collision.
	scopeHash := sha256.Sum256([]byte(scope))
	keyHash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(scopeHash[:]) + ":" + hex.EncodeToString(keyHash[:])
}

// Get returns the stored entry for (scope, key), or nil if absent or expired.
func (s *Store) Get(scope, key string) *Entry {
	id := scopedKey(scope, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return nil
	}
	if e.Expired(s.ttl) {
		delete(s.entries, id)
		return nil
	}
	return e
}

// Set stores an entry for (scope, key). Overwrites any existing entry.
func (s *Store) Set(scope, key string, e *Entry) {
	id := scopedKey(scope, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id] = e
}

// AcquireInflight marks (scope, key) as in-flight. Returns (nil, true) if the
// caller acquired the lock, or (ch, false) if another goroutine is already
// processing the same scoped key — the caller should wait on ch before retrying.
func (s *Store) AcquireInflight(scope, key string) (chan struct{}, bool) {
	id := scopedKey(scope, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, exists := s.inflight[id]; exists {
		return ch, false
	}
	ch := make(chan struct{})
	s.inflight[id] = ch
	return ch, true
}

// ReleaseInflight removes the in-flight lock for (scope, key) and closes the
// channel so waiting goroutines are unblocked.
func (s *Store) ReleaseInflight(scope, key string) {
	id := scopedKey(scope, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, exists := s.inflight[id]; exists {
		close(ch)
		delete(s.inflight, id)
	}
}

// cleanup periodically removes expired entries until Stop is called.
func (s *Store) cleanup() {
	interval := s.ttl / 2
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			for k, e := range s.entries {
				if e.Expired(s.ttl) {
					delete(s.entries, k)
				}
			}
			s.mu.Unlock()
		}
	}
}
