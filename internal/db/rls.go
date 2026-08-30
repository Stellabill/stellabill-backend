package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"stellarbill-backend/internal/middleware"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TenantContextKey string

const DefaultTenantIDKey TenantContextKey = "tenant_id"

var ErrMissingTenantContext = errors.New("missing tenant context: app.tenant_id required for RLS")

func TenantIDFromContext(ctx context.Context) (string, error) {
	if v := ctx.Value(DefaultTenantIDKey); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s, nil
		}
	}
	if v := ctx.Value(middleware.TenantIDKey); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s, nil
		}
	}
	return "", ErrMissingTenantContext
}

func ContextWithTenantID(ctx context.Context, tenantID string) context.Context {
	ctx = context.WithValue(ctx, DefaultTenantIDKey, tenantID)
	ctx = context.WithValue(ctx, middleware.TenantIDKey, tenantID)
	return ctx
}

type RLSPool struct {
	pool *pgxpool.Pool
}

func NewRLSPool(pool *pgxpool.Pool) *RLSPool {
	return &RLSPool{pool: pool}
}

func (p *RLSPool) Underlying() *pgxpool.Pool {
	return p.pool
}

func (p *RLSPool) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}

func (p *RLSPool) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *RLSPool) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx, "BEGIN"); err != nil {
		conn.Release()
		return nil, fmt.Errorf("begin rls transaction: %w", err)
	}
	if _, err := conn.Exec(ctx, "SET LOCAL app.tenant_id = $1", tenantID); err != nil {
		_, _ = conn.Exec(ctx, "ROLLBACK")
		conn.Release()
		return nil, fmt.Errorf("set app.tenant_id: %w", err)
	}
	return conn, nil
}

func (p *RLSPool) ReleaseConnWithRollback(conn *pgxpool.Conn) {
	if conn == nil {
		return
	}
	ctx := context.Background()
	_, _ = conn.Exec(ctx, "ROLLBACK")
	conn.Release()
}

func (p *RLSPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return pgconn.CommandTag{}, fmt.Errorf("begin rls transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SET LOCAL app.tenant_id = $1", tenantID); err != nil {
		return pgconn.CommandTag{}, fmt.Errorf("set app.tenant_id: %w", err)
	}

	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return pgconn.CommandTag{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return pgconn.CommandTag{}, fmt.Errorf("commit rls transaction: %w", err)
	}
	return tag, nil
}

func (p *RLSPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		return nil, fmt.Errorf("begin rls transaction: %w", err)
	}

	if _, err := tx.Exec(ctx, "SET LOCAL app.tenant_id = $1", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		conn.Release()
		return nil, fmt.Errorf("set app.tenant_id: %w", err)
	}

	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		_ = tx.Rollback(ctx)
		conn.Release()
		return nil, err
	}

	return &rlsRows{rows: rows, tx: tx, conn: conn}, nil
}

func (p *RLSPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return &rlsRowError{err: err}
	}
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return &rlsRowError{err: err}
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		return &rlsRowError{err: fmt.Errorf("begin rls transaction: %w", err)}
	}

	if _, err := tx.Exec(ctx, "SET LOCAL app.tenant_id = $1", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		conn.Release()
		return &rlsRowError{err: fmt.Errorf("set app.tenant_id: %w", err)}
	}

	row := tx.QueryRow(ctx, sql, args...)
	return &rlsRow{row: row, tx: tx, conn: conn}
}

func (p *RLSPool) Begin(ctx context.Context) (pgx.Tx, error) {
	return p.BeginTx(ctx, pgx.TxOptions{})
}

func (p *RLSPool) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}

	tx, err := conn.BeginTx(ctx, txOptions)
	if err != nil {
		conn.Release()
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	if _, err := tx.Exec(ctx, "SET LOCAL app.tenant_id = $1", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		conn.Release()
		return nil, fmt.Errorf("set app.tenant_id: %w", err)
	}

	return &rlsTx{tx: tx, conn: conn}, nil
}

type rlsRows struct {
	rows pgx.Rows
	tx   pgx.Tx
	conn *pgxpool.Conn
}

func (r *rlsRows) Close() {
	r.rows.Close()
}

func (r *rlsRows) Err() error {
	return r.rows.Err()
}

func (r *rlsRows) FieldDescriptions() []pgconn.FieldDescription {
	return r.rows.FieldDescriptions()
}

func (r *rlsRows) CommandTag() pgconn.CommandTag {
	return r.rows.CommandTag()
}

func (r *rlsRows) Next() bool {
	hasNext := r.rows.Next()
	if !hasNext {
		_ = r.tx.Rollback(context.Background())
		r.conn.Release()
	}
	return hasNext
}

func (r *rlsRows) Scan(dest ...any) error {
	return r.rows.Scan(dest...)
}

func (r *rlsRows) Values() ([]any, error) {
	return r.rows.Values()
}

func (r *rlsRows) RawValues() [][]byte {
	return r.rows.RawValues()
}

func (r *rlsRows) Conn() *pgx.Conn {
	return r.rows.Conn()
}

type rlsRow struct {
	row     pgx.Row
	tx      pgx.Tx
	conn    *pgxpool.Conn
	scanned bool
}

func (r *rlsRow) Scan(dest ...any) error {
	r.scanned = true
	err := r.row.Scan(dest...)
	defer func() {
		_ = r.tx.Rollback(context.Background())
		r.conn.Release()
	}()
	return err
}

type rlsRowError struct {
	err error
}

func (r *rlsRowError) Scan(_ ...any) error {
	return r.err
}

type rlsTx struct {
	tx   pgx.Tx
	conn *pgxpool.Conn
}

func (t *rlsTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return t.tx.Begin(ctx)
}

func (t *rlsTx) Commit(ctx context.Context) error {
	err := t.tx.Commit(ctx)
	t.conn.Release()
	return err
}

func (t *rlsTx) Rollback(ctx context.Context) error {
	err := t.tx.Rollback(ctx)
	t.conn.Release()
	return err
}

func (t *rlsTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return t.tx.CopyFrom(ctx, tableName, columnNames, rowSrc)
}

func (t *rlsTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return t.tx.SendBatch(ctx, b)
}

func (t *rlsTx) LargeObjects() pgx.LargeObjects {
	return t.tx.LargeObjects()
}

func (t *rlsTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return t.tx.Prepare(ctx, name, sql)
}

func (t *rlsTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return t.tx.Exec(ctx, sql, args...)
}

func (t *rlsTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.tx.Query(ctx, sql, args...)
}

func (t *rlsTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return t.tx.QueryRow(ctx, sql, args...)
}

func (t *rlsTx) Conn() *pgx.Conn {
	return t.tx.Conn()
}

type RLSDB struct {
	db *sql.DB
}

func NewRLSDB(db *sql.DB) *RLSDB {
	return &RLSDB{db: db}
}

func (d *RLSDB) Underlying() *sql.DB {
	return d.db
}

func (d *RLSDB) Close() error {
	return d.db.Close()
}

func (d *RLSDB) PingContext(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

func (d *RLSDB) SetMaxOpenConns(n int) {
	d.db.SetMaxOpenConns(n)
}

func (d *RLSDB) SetMaxIdleConns(n int) {
	d.db.SetMaxIdleConns(n)
}

func (d *RLSDB) SetConnMaxLifetime(v interface{}) {
}

func (d *RLSDB) Stats() sql.DBStats {
	return d.db.Stats()
}

func (d *RLSDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin rls transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "SET LOCAL app.tenant_id = $1", tenantID); err != nil {
		return nil, fmt.Errorf("set app.tenant_id: %w", err)
	}

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit rls transaction: %w", err)
	}
	return result, nil
}

func (d *RLSDB) QueryContext(ctx context.Context, query string, args ...any) (*rlsSQLRows, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin rls transaction: %w", err)
	}

	if _, err := tx.ExecContext(ctx, "SET LOCAL app.tenant_id = $1", tenantID); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("set app.tenant_id: %w", err)
	}

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	return &rlsSQLRows{Rows: rows, tx: tx}, nil
}

func (d *RLSDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		tx, _ := d.db.BeginTx(ctx, nil)
		_ = tx.Rollback()
		return d.db.QueryRowContext(ctx, "SELECT 1 WHERE 1=0")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return d.db.QueryRowContext(ctx, "SELECT 1 WHERE 1=0")
	}

	if _, err := tx.ExecContext(ctx, "SET LOCAL app.tenant_id = $1", tenantID); err != nil {
		_ = tx.Rollback()
		return d.db.QueryRowContext(ctx, "SELECT 1 WHERE 1=0")
	}

	return tx.QueryRowContext(ctx, query, args...)
}

func (d *RLSDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "SET LOCAL app.tenant_id = $1", tenantID); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("set app.tenant_id: %w", err)
	}
	return tx, nil
}

type rlsSQLRows struct {
	*sql.Rows
	tx     *sql.Tx
	closed bool
}

func (r *rlsSQLRows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	err := r.Rows.Close()
	_ = r.tx.Rollback()
	return err
}
