// Package idempotency provides middleware and storage for idempotency key support.
// It prevents duplicate processing of mutating requests by caching responses
// keyed on the Idempotency-Key header value.
package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

const DefaultTTL = 24 * time.Hour

// Entry holds a cached response for a given idempotency key.
type Entry struct {
	StatusCode  int
	Body        []byte
	PayloadHash string // SHA-256 of the original request body
	CreatedAt   time.Time
}

// Expired reports whether the entry has exceeded its TTL.
func (e *Entry) Expired(ttl time.Duration) bool {
	return time.Since(e.CreatedAt) > ttl
}

// Store is a thread-safe in-memory idempotency store.
type Store struct {
	mu      sync.Mutex
	entries map[string]*Entry
	ttl     time.Duration
	// inflight tracks keys currently being processed to handle concurrent duplicates.
	inflight map[string]chan struct{}
}

// NewStore creates a Store with the given TTL and starts a background cleanup goroutine.
func NewStore(ttl time.Duration) *Store {
	s := &Store{
		entries:  make(map[string]*Entry),
		inflight: make(map[string]chan struct{}),
		ttl:      ttl,
	}
	go s.cleanup()
	return s
}

// HashPayload returns a hex-encoded SHA-256 hash of the given bytes.
func HashPayload(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Get returns the stored entry for key, or nil if absent or expired.
func (s *Store) Get(key string) *Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return nil
	}
	if e.Expired(s.ttl) {
		delete(s.entries, key)
		return nil
	}
	return e
}

// Set stores an entry for key. Overwrites any existing entry.
func (s *Store) Set(key string, e *Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = e
}

// AcquireInflight marks key as in-flight. Returns (nil, true) if the caller
// acquired the lock, or (ch, false) if another goroutine is already processing
// the same key — the caller should wait on ch before retrying.
func (s *Store) AcquireInflight(key string) (chan struct{}, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, exists := s.inflight[key]; exists {
		return ch, false
	}
	ch := make(chan struct{})
	s.inflight[key] = ch
	return ch, true
}

// ReleaseInflight removes the in-flight lock for key and closes the channel
// so waiting goroutines are unblocked.
func (s *Store) ReleaseInflight(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, exists := s.inflight[key]; exists {
		close(ch)
		delete(s.inflight, key)
	}
}

// cleanup periodically removes expired entries.
func (s *Store) cleanup() {
	ticker := time.NewTicker(s.ttl / 2)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for k, e := range s.entries {
			if e.Expired(s.ttl) {
				delete(s.entries, k)
			}
		}
		s.mu.Unlock()
	}
}
