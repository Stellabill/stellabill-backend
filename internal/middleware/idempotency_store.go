package middleware

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrRequestMismatch is returned when an idempotency key is reused with a different request.
var ErrRequestMismatch = errors.New("idempotency key reused with a different request")

// IdempotencyStore defines the contract for persisting idempotency keys and request states.
type IdempotencyStore interface {
	GetOrInsert(ctx context.Context, scope, key, method, path, payloadHash string, ttl time.Duration) (statusCode int, responseBody []byte, isReplay bool, isInFlight bool, err error)
	UpdateResponse(ctx context.Context, scope, key string, statusCode int, responseBody []byte) error
	Delete(ctx context.Context, scope, key string) error
	DeleteExpiredBatch(ctx context.Context, batchSize int) (int64, error)
	CountExpiredPending(ctx context.Context) (int64, error)
}

// PostgresIdempotencyStore implements IdempotencyStore backed by PostgreSQL.
type PostgresIdempotencyStore struct {
	pool *pgxpool.Pool
}

// NewPostgresIdempotencyStore creates a new PostgresIdempotencyStore.
func NewPostgresIdempotencyStore(pool *pgxpool.Pool) *PostgresIdempotencyStore {
	return &PostgresIdempotencyStore{pool: pool}
}

// GetOrInsert checks if the key exists. If it does not exist (or has expired), it inserts an in-flight placeholder.
func (s *PostgresIdempotencyStore) GetOrInsert(ctx context.Context, scope, key, method, path, payloadHash string, ttl time.Duration) (statusCode int, responseBody []byte, isReplay bool, isInFlight bool, err error) {
	if s.pool == nil {
		return 0, nil, false, false, errors.New("postgres connection pool is nil")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, nil, false, false, err
	}
	defer tx.Rollback(ctx)

	var (
		storedMethod      string
		storedPath        string
		storedPayloadHash string
		storedStatus      int
		storedBody        []byte
		expiresAt         time.Time
	)

	qLookup := `
		SELECT method, path, payload_hash, status_code, response_body, expires_at
		FROM idempotency_keys
		WHERE scope = $1 AND key = $2
		FOR UPDATE`

	err = tx.QueryRow(ctx, qLookup, scope, key).Scan(
		&storedMethod, &storedPath, &storedPayloadHash, &storedStatus, &storedBody, &expiresAt,
	)

	now := time.Now()

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// First request, insert in-flight record
			expires := now.Add(ttl)
			qInsert := `
				INSERT INTO idempotency_keys (scope, key, method, path, payload_hash, status_code, response_body, expires_at)
				VALUES ($1, $2, $3, $4, $5, 0, $6, $7)`
			_, err = tx.Exec(ctx, qInsert, scope, key, method, path, payloadHash, []byte{}, expires)
			if err != nil {
				return 0, nil, false, false, err
			}

			if err = tx.Commit(ctx); err != nil {
				return 0, nil, false, false, err
			}
			return 0, nil, false, false, nil
		}
		return 0, nil, false, false, err
	}

	// Record found, check expiration
	if now.After(expiresAt) {
		// Key has expired. Reuse the key and update it to be in-flight
		expires := now.Add(ttl)
		qUpdateExpired := `
			UPDATE idempotency_keys
			SET method = $3, path = $4, payload_hash = $5, status_code = 0, response_body = $6, expires_at = $7, created_at = now()
			WHERE scope = $1 AND key = $2`
		_, err = tx.Exec(ctx, qUpdateExpired, scope, key, method, path, payloadHash, []byte{}, expires)
		if err != nil {
			return 0, nil, false, false, err
		}

		if err = tx.Commit(ctx); err != nil {
			return 0, nil, false, false, err
		}
		return 0, nil, false, false, nil
	}

	// Key is valid and active.
	if storedStatus == 0 {
		// Concurrent request in progress
		return 0, nil, false, true, nil
	}

	// Completed duplicate request. Verify mismatch
	if storedMethod != method || storedPath != path || storedPayloadHash != payloadHash {
		return 0, nil, false, false, ErrRequestMismatch
	}

	return storedStatus, storedBody, true, false, nil
}

// UpdateResponse updates the cached response status and body in the database.
func (s *PostgresIdempotencyStore) UpdateResponse(ctx context.Context, scope, key string, statusCode int, responseBody []byte) error {
	if s.pool == nil {
		return errors.New("postgres connection pool is nil")
	}

	qUpdate := `
		UPDATE idempotency_keys
		SET status_code = $3, response_body = $4
		WHERE scope = $1 AND key = $2`
	_, err := s.pool.Exec(ctx, qUpdate, scope, key, statusCode, responseBody)
	return err
}

// Delete removes an idempotency key, e.g., when a request fails with a non-2xx status code.
func (s *PostgresIdempotencyStore) Delete(ctx context.Context, scope, key string) error {
	if s.pool == nil {
		return errors.New("postgres connection pool is nil")
	}

	qDelete := `
		DELETE FROM idempotency_keys
		WHERE scope = $1 AND key = $2`
	_, err := s.pool.Exec(ctx, qDelete, scope, key)
	return err
}

// DeleteExpiredBatch deletes expired keys in batches.
func (s *PostgresIdempotencyStore) DeleteExpiredBatch(ctx context.Context, batchSize int) (int64, error) {
	if s.pool == nil {
		return 0, errors.New("postgres connection pool is nil")
	}

	qDelete := `
		DELETE FROM idempotency_keys 
		WHERE id IN (
			SELECT id FROM idempotency_keys 
			WHERE expires_at <= NOW() 
			FOR UPDATE SKIP LOCKED 
			LIMIT $1
		)`

	cmdTag, err := s.pool.Exec(ctx, qDelete, batchSize)
	if err != nil {
		return 0, err
	}
	return cmdTag.RowsAffected(), nil
}

// CountExpiredPending counts how many keys are currently past their TTL.
func (s *PostgresIdempotencyStore) CountExpiredPending(ctx context.Context) (int64, error) {
	if s.pool == nil {
		return 0, errors.New("postgres connection pool is nil")
	}

	qCount := `SELECT COUNT(*) FROM idempotency_keys WHERE expires_at <= NOW()`
	
	var count int64
	err := s.pool.QueryRow(ctx, qCount).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// InMemoryIdempotencyEntry represents a single cached item.
type InMemoryIdempotencyEntry struct {
	method       string
	path         string
	payloadHash  string
	statusCode   int
	responseBody []byte
	expiresAt    time.Time
}

// InMemoryIdempotencyStore implements IdempotencyStore in memory (for testing and fallback).
type InMemoryIdempotencyStore struct {
	mu   sync.RWMutex
	keys map[string]*InMemoryIdempotencyEntry
}

// NewInMemoryIdempotencyStore creates a new InMemoryIdempotencyStore.
func NewInMemoryIdempotencyStore() *InMemoryIdempotencyStore {
	return &InMemoryIdempotencyStore{
		keys: make(map[string]*InMemoryIdempotencyEntry),
	}
}

// GetOrInsert checks if the key exists in memory. If not (or expired), inserts a placeholder.
func (s *InMemoryIdempotencyStore) GetOrInsert(ctx context.Context, scope, key, method, path, payloadHash string, ttl time.Duration) (statusCode int, responseBody []byte, isReplay bool, isInFlight bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	mapKey := scope + "/" + key
	entry, exists := s.keys[mapKey]
	now := time.Now()

	if exists && now.After(entry.expiresAt) {
		delete(s.keys, mapKey)
		exists = false
	}

	if !exists {
		s.keys[mapKey] = &InMemoryIdempotencyEntry{
			method:      method,
			path:        path,
			payloadHash: payloadHash,
			statusCode:  0,
			expiresAt:   now.Add(ttl),
		}
		return 0, nil, false, false, nil
	}

	if entry.statusCode == 0 {
		return 0, nil, false, true, nil
	}

	if entry.method != method || entry.path != path || entry.payloadHash != payloadHash {
		return 0, nil, false, false, ErrRequestMismatch
	}

	return entry.statusCode, entry.responseBody, true, false, nil
}

// UpdateResponse updates the cached status and response body in memory.
func (s *InMemoryIdempotencyStore) UpdateResponse(ctx context.Context, scope, key string, statusCode int, responseBody []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mapKey := scope + "/" + key
	if entry, exists := s.keys[mapKey]; exists {
		entry.statusCode = statusCode
		entry.responseBody = responseBody
	}
	return nil
}

// Delete deletes the entry from memory.
func (s *InMemoryIdempotencyStore) Delete(ctx context.Context, scope, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mapKey := scope + "/" + key
	delete(s.keys, mapKey)
	return nil
}

// DeleteExpiredBatch deletes expired keys in memory up to batchSize.
func (s *InMemoryIdempotencyStore) DeleteExpiredBatch(ctx context.Context, batchSize int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var deleted int64
	now := time.Now()
	for k, entry := range s.keys {
		if deleted >= int64(batchSize) {
			break
		}
		if now.After(entry.expiresAt) {
			delete(s.keys, k)
			deleted++
		}
	}
	return deleted, nil
}

// CountExpiredPending counts expired keys in memory.
func (s *InMemoryIdempotencyStore) CountExpiredPending(ctx context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int64
	now := time.Now()
	for _, entry := range s.keys {
		if now.After(entry.expiresAt) {
			count++
		}
	}
	return count, nil
}

