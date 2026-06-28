package db

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

type contextKey string

const (
	// FreshnessTokenKey is the context key for the read-your-writes freshness token.
	FreshnessTokenKey contextKey = "freshness_token"
)

// Pinger defines an interface to ping a database connection or pool.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// ReadRouter routes read queries to a read replica or primary database pool.
// It implements the DBTX interface, directing safe read context calls to the replica.
type ReadRouter struct {
	primary DBTX
	replica DBTX

	// Failover configuration
	mu              sync.RWMutex
	replicaDown     bool
	lastCheck       time.Time
	pingTimeout     time.Duration
	healthCheckFreq time.Duration
}

// NewReadRouter creates a new ReadRouter with primary and replica connections.
func NewReadRouter(primary, replica DBTX) *ReadRouter {
	return &ReadRouter{
		primary:         primary,
		replica:         replica,
		pingTimeout:     50 * time.Millisecond,
		healthCheckFreq: 5 * time.Second,
	}
}

// WithFreshnessToken returns a new context with the freshness token attached.
func WithFreshnessToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, FreshnessTokenKey, token)
}

// Reader selects the appropriate connection (primary vs replica) based on the context.
// It enforces read-your-writes consistency when a freshness token is present.
// It automatically falls back to primary if the replica is nil or determined to be down.
func (r *ReadRouter) Reader(ctx context.Context) DBTX {
	if ctx == nil {
		return r.primary
	}

	// 1. Check for freshness token (read-your-writes)
	if val := ctx.Value(FreshnessTokenKey); val != nil {
		if token, ok := val.(string); ok && token != "" {
			return r.primary
		}
	}

	if r.replica == nil {
		return r.primary
	}

	// 2. Check replica health (with simple failover caching)
	r.mu.RLock()
	isDown := r.replicaDown
	last := r.lastCheck
	r.mu.RUnlock()

	if isDown && time.Since(last) < r.healthCheckFreq {
		// Replica is marked down and we checked recently; failover to primary
		return r.primary
	}

	// Check if we should re-verify health
	if time.Since(last) >= r.healthCheckFreq {
		r.mu.Lock()
		// Double check under write lock
		if time.Since(r.lastCheck) >= r.healthCheckFreq {
			r.lastCheck = time.Now()

			// Try to ping the replica (if it supports Pinger interface or is *sql.DB)
			var pingErr error
			if p, ok := r.replica.(Pinger); ok {
				pingCtx, cancel := context.WithTimeout(ctx, r.pingTimeout)
				pingErr = p.PingContext(pingCtx)
				cancel()
			} else if dbHandle, ok := r.replica.(*sql.DB); ok {
				pingCtx, cancel := context.WithTimeout(ctx, r.pingTimeout)
				pingErr = dbHandle.PingContext(pingCtx)
				cancel()
			}

			if pingErr != nil {
				r.replicaDown = true
			} else {
				r.replicaDown = false
			}
		}
		isDown = r.replicaDown
		r.mu.Unlock()
	}

	if isDown {
		return r.primary
	}

	return r.replica
}

// ExecContext routes writes to the primary pool.
func (r *ReadRouter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return r.primary.ExecContext(ctx, query, args...)
}

// PrepareContext routes to the primary pool for prepared-statement compatibility.
func (r *ReadRouter) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return r.primary.PrepareContext(ctx, query)
}

// QueryContext routes reads to the Reader.
func (r *ReadRouter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return r.Reader(ctx).QueryContext(ctx, query, args...)
}

// QueryRowContext routes reads to the Reader.
func (r *ReadRouter) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return r.Reader(ctx).QueryRowContext(ctx, query, args...)
}

// Exec routes writes to the primary pool.
func (r *ReadRouter) Exec(query string, args ...any) (sql.Result, error) {
	return r.primary.Exec(query, args...)
}

// Query routes to the primary pool (context-less safe fallback).
func (r *ReadRouter) Query(query string, args ...any) (*sql.Rows, error) {
	return r.primary.Query(query, args...)
}

// QueryRow routes to the primary pool (context-less safe fallback).
func (r *ReadRouter) QueryRow(query string, args ...any) *sql.Row {
	return r.primary.QueryRow(query, args...)
}
