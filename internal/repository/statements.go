package repository

import (
    "context"
    "database/sql"
    "errors"
    "time"
)

// StatementsRepo handles CRUD operations on the partitioned statements table.
// The table is partitioned by tenant_id and month (created_at), so every query
// must include tenant_id to leverage partition pruning and enforce isolation.
type StatementsRepo struct {
    db *sql.DB
}

// NewStatementsRepo creates a new StatementsRepo.
func NewStatementsRepo(db *sql.DB) *StatementsRepo {
    return &StatementsRepo{db: db}
}

// Statement represents a row in the statements table.
type Statement struct {
    ID        int64     `db:"id"`
    TenantID  string    `db:"tenant_id"`
    Content   string    `db:"content"`
    CreatedAt time.Time `db:"created_at"`
    UpdatedAt time.Time `db:"updated_at"`
}

// ErrInvalidInput is returned when input parameters are invalid.
var ErrInvalidInput = errors.New("invalid input")

// Create inserts a new statement and returns its ID.
// The tenant ID must be non-empty. The created_at field is set to the current time
// if not provided, ensuring the correct monthly partition is selected.
func (r *StatementsRepo) Create(ctx context.Context, s *Statement) (int64, error) {
    if s == nil || s.TenantID == "" {
        return 0, ErrInvalidInput
    }

    if s.CreatedAt.IsZero() {
        s.CreatedAt = time.Now().UTC()
    }
    s.UpdatedAt = s.CreatedAt

    query := `
        INSERT INTO statements (tenant_id, content, created_at, updated_at)
        VALUES (?, ?, ?, ?)
        RETURNING id`
    var id int64
    err := r.db.QueryRowContext(ctx, query, s.TenantID, s.Content, s.CreatedAt, s.UpdatedAt).Scan(&id)
    if err != nil {
        return 0, err
    }
    return id, nil
}

// Get retrieves a statement by tenant ID and ID.
// Both tenant_id and id are required to target the correct partition.
func (r *StatementsRepo) Get(ctx context.Context, tenantID string, id int64) (*Statement, error) {
    if tenantID == "" || id <= 0 {
        return nil, ErrInvalidInput
    }

    query := `
        SELECT id, tenant_id, content, created_at, updated_at
        FROM statements
        WHERE tenant_id = ? AND id = ?`
    var st Statement
    err := r.db.QueryRowContext(ctx, query, tenantID, id).Scan(&st.ID, &st.TenantID, &st.Content, &st.CreatedAt, &st.UpdatedAt)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, ErrNotFound
    }
    if err != nil {
        return nil, err
    }
    return &st, nil
}

// List returns statements for a tenant within an optional time range [from, to].
// The tenant_id is always required. Time boundaries are inclusive.
func (r *StatementsRepo) List(ctx context.Context, tenantID string, from, to *time.Time) ([]Statement, error) {
    if tenantID == "" {
        return nil, ErrInvalidInput
    }

    query := `
        SELECT id, tenant_id, content, created_at, updated_at
        FROM statements
        WHERE tenant_id = ?`
    args := []interface{}{tenantID}

    if from != nil {
        query += " AND created_at >= ?"
        args = append(args, *from)
    }
    if to != nil {
        query += " AND created_at <= ?"
        args = append(args, *to)
    }

    // Order by created_at for consistent pagination.
    query += " ORDER BY created_at"

    rows, err := r.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var statements []Statement
    for rows.Next() {
        var st Statement
        if err := rows.Scan(&st.ID, &st.TenantID, &st.Content, &st.CreatedAt, &st.UpdatedAt); err != nil {
            return nil, err
        }
        statements = append(statements, st)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return statements, nil
}

// Update modifies an existing statement. It validates tenant ownership.
func (r *StatementsRepo) Update(ctx context.Context, tenantID string, id int64, content string) error {
    if tenantID == "" || id <= 0 {
        return ErrInvalidInput
    }

    query := `
        UPDATE statements
        SET content = ?, updated_at = ?
        WHERE tenant_id = ? AND id = ?`
    res, err := r.db.ExecContext(ctx, query, content, time.Now().UTC(), tenantID, id)
    if err != nil {
        return err
    }
    affected, err := res.RowsAffected()
    if err != nil {
        return err
    }
    if affected == 0 {
        return ErrNotFound
    }
    return nil
}

// Delete removes a statement. It returns ErrNotFound if no row was deleted.
func (r *StatementsRepo) Delete(ctx context.Context, tenantID string, id int64) error {
    if tenantID == "" || id <= 0 {
        return ErrInvalidInput
    }

    query := `
        DELETE FROM statements
        WHERE tenant_id = ? AND id = ?`
    res, err := r.db.ExecContext(ctx, query, tenantID, id)
    if err != nil {
        return err
    }
    affected, err := res.RowsAffected()
    if err != nil {
        return err
    }
    if affected == 0 {
        return ErrNotFound
    }
    return nil
}
