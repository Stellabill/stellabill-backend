// Package idempotency provides middleware and storage for idempotency key support.
// It prevents duplicate processing of mutating requests by caching responses
// keyed on the Idempotency-Key header value.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// IdempotencyResult represents the outcome of trying to acquire an idempotency key.
type IdempotencyResult struct {
	Status          string
	ResponseCode    int
	ResponseBody    []byte
	ResponseHeaders map[string][]string
}

// pgxPoolConn abstracts the PostgreSQL connection pool methods used by DBStore, enabling testing via mocks.
type pgxPoolConn interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DBStore implements a PostgreSQL database-backed store for idempotency keys.
type DBStore struct {
	pool pgxPoolConn
	ttl  time.Duration
}

// NewDBStore creates a new DBStore instance.
func NewDBStore(pool pgxPoolConn, ttl time.Duration) *DBStore {
	return &DBStore{
		pool: pool,
		ttl:  ttl,
	}
}

// Acquire atomically checks the state of an idempotency key.
// Returns "in_flight" if the key is newly acquired.
// Returns "in_flight_duplicate" if the key is currently being processed by another caller.
// Returns "payload_mismatch" if the key is completed but with a different payload hash.
// Returns "completed" with the cached response if the request was already processed.
func (s *DBStore) Acquire(ctx context.Context, scope, key, payloadHash string) (*IdempotencyResult, error) {
	// Delete expired key if any
	_, err := s.pool.Exec(ctx, "DELETE FROM idempotency_keys WHERE scope = $1 AND key = $2 AND expires_at < now()", scope, key)
	if err != nil {
		return nil, fmt.Errorf("delete expired key: %w", err)
	}

	// Try to insert in_flight key
	expiresAt := time.Now().Add(s.ttl)
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO idempotency_keys (scope, key, status, payload_hash, expires_at)
		VALUES ($1, $2, 'in_flight', $3, $4)
		ON CONFLICT (scope, key) DO NOTHING
	`, scope, key, payloadHash, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("insert idempotency key: %w", err)
	}

	// If we inserted 1 row, we acquired the lock!
	if tag.RowsAffected() == 1 {
		return &IdempotencyResult{Status: "in_flight"}, nil
	}

	// Otherwise, check status of the existing key
	var status string
	var responseCode *int
	var responseBody []byte
	var responseHeaders []byte
	var existingHash string

	err = s.pool.QueryRow(ctx, `
		SELECT status, response_code, response_body, response_headers, payload_hash
		FROM idempotency_keys
		WHERE scope = $1 AND key = $2
	`, scope, key).Scan(&status, &responseCode, &responseBody, &responseHeaders, &existingHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Row was deleted concurrently, retry acquisition
			return s.Acquire(ctx, scope, key, payloadHash)
		}
		return nil, fmt.Errorf("query idempotency key: %w", err)
	}

	if status == "in_flight" {
		return &IdempotencyResult{Status: "in_flight_duplicate"}, nil
	}

	if existingHash != payloadHash {
		return &IdempotencyResult{Status: "payload_mismatch"}, nil
	}

	var headers map[string][]string
	if len(responseHeaders) > 0 {
		if err := json.Unmarshal(responseHeaders, &headers); err != nil {
			return nil, fmt.Errorf("unmarshal response headers: %w", err)
		}
	}

	code := 200
	if responseCode != nil {
		code = *responseCode
	}

	return &IdempotencyResult{
		Status:          "completed",
		ResponseCode:    code,
		ResponseBody:    responseBody,
		ResponseHeaders: headers,
	}, nil
}

// SaveResponse marks a key as completed and caches the status code, body, and headers.
func (s *DBStore) SaveResponse(ctx context.Context, scope, key string, statusCode int, body []byte, headers map[string][]string) error {
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		return fmt.Errorf("marshal response headers: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		UPDATE idempotency_keys
		SET status = 'completed',
		    response_code = $3,
		    response_body = $4,
		    response_headers = $5
		WHERE scope = $1 AND key = $2
	`, scope, key, statusCode, body, headersJSON)
	if err != nil {
		return fmt.Errorf("update idempotency key: %w", err)
	}
	return nil
}

// DeleteKey removes the key from the database, unlocking it for retries.
func (s *DBStore) DeleteKey(ctx context.Context, scope, key string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM idempotency_keys WHERE scope = $1 AND key = $2", scope, key)
	if err != nil {
		return fmt.Errorf("delete idempotency key: %w", err)
	}
	return nil
}
