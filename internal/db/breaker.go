package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sony/gobreaker"
)

// BreakerPool wraps a *pgxpool.Pool with a circuit breaker to prevent cascading failures.
type BreakerPool struct {
	pool    *pgxpool.Pool
	breaker *gobreaker.CircuitBreaker
}

// NewBreakerPool creates a new BreakerPool using the provided pool and circuit breaker configuration.
func NewBreakerPool(pool *pgxpool.Pool, maxFailures uint32, timeoutSeconds uint32, halfOpenMaxRequests uint32) *BreakerPool {
	settings := gobreaker.Settings{
		Name:        "db-pool",
		MaxRequests: halfOpenMaxRequests,
		Interval:    0,
		Timeout:     time.Duration(timeoutSeconds) * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= maxFailures
		},
		IsSuccessful: func(err error) bool {
			return err == nil
		},
	}
	return &BreakerPool{
		pool:    pool,
		breaker: gobreaker.NewCircuitBreaker(settings),
	}
}

// Pool returns the underlying *pgxpool.Pool (for cases where raw access is needed).
func (b *BreakerPool) Pool() *pgxpool.Pool {
	return b.pool
}

// State returns the current state of the circuit breaker.
func (b *BreakerPool) State() gobreaker.State {
	return b.breaker.State()
}

// Counts returns the current breaker counts.
func (b *BreakerPool) Counts() gobreaker.Counts {
	return b.breaker.Counts()
}

// PingContext pings the database through the circuit breaker.
func (b *BreakerPool) PingContext(ctx context.Context) error {
	_, err := b.breaker.Execute(func() (interface{}, error) {
		return nil, b.pool.Ping(ctx)
	})
	return err
}

// Query executes a query on the database through the circuit breaker.
func (b *BreakerPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	res, err := b.breaker.Execute(func() (interface{}, error) {
		return b.pool.Query(ctx, sql, args...)
	})
	if err != nil {
		return nil, err
	}
	return res.(pgx.Rows), nil
}

// QueryRow executes a query that is expected to return at most one row, through the circuit breaker.
func (b *BreakerPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	// Need to handle this differently since pgx.Row never returns an error directly,
	// but returns an error when Scan is called. For circuit breaker to work, we need to
	// wrap the row so that scan reports errors to breaker.
	row := b.pool.QueryRow(ctx, sql, args...)
	return &breakerRow{row: row, breaker: b.breaker}
}

type breakerRow struct {
	row     pgx.Row
	breaker *gobreaker.CircuitBreaker
}

func (br *breakerRow) Scan(dest ...any) error {
	var err error
	_, _ = br.breaker.Execute(func() (interface{}, error) {
		err = br.row.Scan(dest...)
		if err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

// Exec executes a query that doesn't return rows, through the circuit breaker.
func (b *BreakerPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	res, err := b.breaker.Execute(func() (interface{}, error) {
		return b.pool.Exec(ctx, sql, args...)
	})
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return res.(pgconn.CommandTag), nil
}

// Begin starts a transaction through the circuit breaker.
func (b *BreakerPool) Begin(ctx context.Context) (pgx.Tx, error) {
	res, err := b.breaker.Execute(func() (interface{}, error) {
		return b.pool.Begin(ctx)
	})
	if err != nil {
		return nil, err
	}
	return &breakerTx{tx: res.(pgx.Tx), breaker: b.breaker}, nil
}

type breakerTx struct {
	tx      pgx.Tx
	breaker *gobreaker.CircuitBreaker
}

func (bt *breakerTx) Begin(ctx context.Context) (pgx.Tx, error) {
	var nestedTx pgx.Tx
	var err error
	_, _ = bt.breaker.Execute(func() (interface{}, error) {
		nestedTx, err = bt.tx.Begin(ctx)
		if err != nil {
			return nil, err
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	return &breakerTx{tx: nestedTx, breaker: bt.breaker}, nil
}

func (bt *breakerTx) Commit(ctx context.Context) error {
	var err error
	_, _ = bt.breaker.Execute(func() (interface{}, error) {
		err = bt.tx.Commit(ctx)
		if err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func (bt *breakerTx) Rollback(ctx context.Context) error {
	var err error
	_, _ = bt.breaker.Execute(func() (interface{}, error) {
		err = bt.tx.Rollback(ctx)
		if err != nil && err != pgx.ErrTxClosed {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func (bt *breakerTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	res, err := bt.breaker.Execute(func() (interface{}, error) {
		return bt.tx.Query(ctx, sql, args...)
	})
	if err != nil {
		return nil, err
	}
	return res.(pgx.Rows), nil
}

func (bt *breakerTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	row := bt.tx.QueryRow(ctx, sql, args...)
	return &breakerRow{row: row, breaker: bt.breaker}
}

func (bt *breakerTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	res, err := bt.breaker.Execute(func() (interface{}, error) {
		return bt.tx.Exec(ctx, sql, args...)
	})
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return res.(pgconn.CommandTag), nil
}

func (bt *breakerTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	var stmt *pgconn.StatementDescription
	var err error
	_, _ = bt.breaker.Execute(func() (interface{}, error) {
		stmt, err = bt.tx.Prepare(ctx, name, sql)
		if err != nil {
			return nil, err
		}
		return nil, nil
	})
	return stmt, err
}

func (bt *breakerTx) LargeObjects() pgx.LargeObjects {
	return bt.tx.LargeObjects()
}

func (bt *breakerTx) Conn() *pgx.Conn {
	return bt.tx.Conn()
}

func (bt *breakerTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	var count int64
	var err error
	_, _ = bt.breaker.Execute(func() (interface{}, error) {
		count, err = bt.tx.CopyFrom(ctx, tableName, columnNames, rowSrc)
		if err != nil {
			return nil, err
		}
		return nil, nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (bt *breakerTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	// Batches are more complex, for simplicity we'll just forward but we can't easily wrap the batch results
	// since they have multiple methods that can return errors. This is a limitation.
	return bt.tx.SendBatch(ctx, b)
}

func (b *BreakerPool) Close() {
	b.pool.Close()
}

func (b *BreakerPool) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	res, err := b.breaker.Execute(func() (interface{}, error) {
		return b.pool.Acquire(ctx)
	})
	if err != nil {
		return nil, err
	}
	conn := res.(*pgxpool.Conn)
	return conn, nil
}

func (b *BreakerPool) Stat() *pgxpool.Stat {
	return b.pool.Stat()
}

func (b *BreakerPool) Reset() {
	b.pool.Reset()
}

func (b *BreakerPool) Config() *pgxpool.Config {
	return b.pool.Config()
}

// Ping implements DBPinger interface for health checks
func (b *BreakerPool) Ping(ctx context.Context) error {
	_, err := b.breaker.Execute(func() (interface{}, error) {
		return nil, b.pool.Ping(ctx)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) {
			return fmt.Errorf("circuit breaker open: %w", err)
		}
		return err
	}
	return nil
}
