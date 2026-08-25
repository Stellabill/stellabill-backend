package db

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/secrets"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RotatingPool wraps a *pgxpool.Pool whose credentials are rotated by a
// background task (secrets.DBRenewer). When new credentials arrive on the
// channel, the old pool is drained and a new one is opened with the fresh
// credentials, so rotation happens without a pod restart.
//
// The pool is safe for concurrent use: callers acquire the current pool via
// Pool() and release it after use. The underlying pool is swapped atomically
// under a read-write lock.
type RotatingPool struct {
	mu     sync.RWMutex
	pool   *pgxpool.Pool
	cfg    config.Config
	creds  <-chan secrets.DBCredential
	closed bool
}

// NewRotatingPool builds a RotatingPool that waits for the first credential
// on the channel, opens a pool with it, and re-opens the pool whenever a new
// credential arrives. The provided ctx bounds the initial connectivity check.
//
// When cfg.DBConn is empty (e.g. local dev with no DATABASE_URL) it returns
// (nil, nil) so callers can degrade gracefully.
func NewRotatingPool(ctx context.Context, cfg config.Config, creds <-chan secrets.DBCredential) (*RotatingPool, error) {
	if cfg.DBConn == "" {
		return nil, nil
	}
	if creds == nil {
		return nil, fmt.Errorf("credential channel is nil")
	}

	rp := &RotatingPool{
		cfg:   cfg,
		creds: creds,
	}

	// Wait for the first credential before returning so the pool is usable
	// immediately.
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for initial db credential: %w", ctx.Err())
	case cred, ok := <-creds:
		if !ok {
			return nil, fmt.Errorf("credential channel closed before first credential")
		}
		pool, err := rp.openPool(ctx, cred)
		if err != nil {
			return nil, err
		}
		rp.pool = pool
	}

	go rp.watch(ctx)
	return rp, nil
}

// Pool returns the current underlying *pgxpool.Pool. It may be nil if the
// pool has been closed. Callers should treat the returned pool as valid only
// for the duration of the call.
func (r *RotatingPool) Pool() *pgxpool.Pool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pool
}

// Close drains and closes the current pool and stops the rotation loop.
func (r *RotatingPool) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.pool != nil {
		return DrainPool(ctx, r.pool)
	}
	return nil
}

// watch listens for new credentials and re-opens the pool when they arrive.
func (r *RotatingPool) watch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case cred, ok := <-r.creds:
			if !ok {
				return
			}
			r.rotate(ctx, cred)
		}
	}
}

// rotate swaps the current pool for a new one built with the given credential.
// If opening the new pool fails, the old pool is kept so existing connections
// continue to work until the credential actually expires.
func (r *RotatingPool) rotate(ctx context.Context, cred secrets.DBCredential) {
	newPool, err := r.openPool(ctx, cred)
	if err != nil {
		// Keep the existing pool; the credential may still be valid until
		// its expiry. The renewer will hard-fail if it cannot renew in time.
		return
	}

	r.mu.Lock()
	old := r.pool
	r.pool = newPool
	r.mu.Unlock()

	if old != nil {
		// Drain the old pool in the background so in-flight queries can
		// finish before the old credentials are revoked.
		go func() {
			drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = DrainPool(drainCtx, old)
		}()
	}
}

// openPool builds a pgxpool.Pool from cfg with the given dynamic credential
// substituted into the connection string, and verifies connectivity.
func (r *RotatingPool) openPool(ctx context.Context, cred secrets.DBCredential) (*pgxpool.Pool, error) {
	dsn, err := buildCredentialDSN(r.cfg.DBConn, cred)
	if err != nil {
		return nil, err
	}

	cfg := r.cfg
	cfg.DBConn = dsn
	poolCfg, err := NewPoolConfig(cfg)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create rotating database pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping rotating database pool: %w", err)
	}

	return pool, nil
}

// buildCredentialDSN replaces the user and password in the base DSN with the
// dynamic credential while preserving host, port, database, and sslmode.
func buildCredentialDSN(base string, cred secrets.DBCredential) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse base dsn: %w", err)
	}
	u.User = url.UserPassword(cred.Username, cred.Password)
	return u.String(), nil
}
