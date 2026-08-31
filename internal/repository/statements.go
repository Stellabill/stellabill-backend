package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Statement struct {
	ID        int64
	TenantID  string
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

var (
	ErrNotFound       = errors.New("not found")
	ErrInvalidInput   = errors.New("invalid input")
	ErrConcurrentEdit = errors.New("concurrent edit")
)

// StatementsRepo provides statement operations.
type StatementsRepo struct {
	db *sql.DB
}

func NewStatementsRepo(db *sql.DB) *StatementsRepo {
	return &StatementsRepo{db: db}
}

func (r *StatementsRepo) Create(ctx context.Context, s *Statement) (int64, error) {
	if s.TenantID == "" || s.Content == "" {
		return 0, ErrInvalidInput
	}

	s.CreatedAt = time.Now().UTC()
	s.UpdatedAt = s.CreatedAt

	query := `
		INSERT INTO statements (tenant_id, content, created_at, updated_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id`

	var id int64
	err := r.db.QueryRowContext(ctx, query, s.TenantID, s.Content, s.CreatedAt, s.UpdatedAt).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create statement: %w", err)
	}
	s.ID = id
	return id, nil
}

func (r *StatementsRepo) Get(ctx context.Context, tenantID string, id int64) (*Statement, error) {
	if tenantID == "" {
		return nil, ErrInvalidInput
	}

	query := `
		SELECT id, tenant_id, content, created_at, updated_at
		FROM statements
		WHERE id = $1 AND tenant_id = $2`

	var s Statement
	err := r.db.QueryRowContext(ctx, query, id, tenantID).Scan(&s.ID, &s.TenantID, &s.Content, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get statement: %w", err)
	}
	return &s, nil
}

func (r *StatementsRepo) List(ctx context.Context, tenantID string, from, to *time.Time) ([]Statement, error) {
	if tenantID == "" {
		return nil, ErrInvalidInput
	}

	query := `
		SELECT id, tenant_id, content, created_at, updated_at
		FROM statements
		WHERE tenant_id = $1`
	args := []interface{}{tenantID}
	argIdx := 2

	if from != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, *from)
		argIdx++
	}
	if to != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, *to)
		argIdx++
	}

	query += " ORDER BY created_at"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list statements: %w", err)
	}
	defer rows.Close()

	var statements []Statement
	for rows.Next() {
		var s Statement
		if err := rows.Scan(&s.ID, &s.TenantID, &s.Content, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan statement: %w", err)
		}
		statements = append(statements, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return statements, nil
}

func (r *StatementsRepo) Update(ctx context.Context, s *Statement) error {
	if s.ID == 0 || s.TenantID == "" {
		return ErrInvalidInput
	}

	query := `
		UPDATE statements
		SET content = $1, updated_at = $2
		WHERE id = $3 AND tenant_id = $3`

	s.UpdatedAt = time.Now().UTC()
	result, err := r.db.ExecContext(ctx, query, s.Content, s.UpdatedAt, s.ID, s.TenantID)
	if err != nil {
		return fmt.Errorf("update statement: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return ErrConcurrentEdit
	}
	return nil
}

func (r *StatementsRepo) Delete(ctx context.Context, tenantID string, id int64) error {
	if tenantID == "" || id == 0 {
		return ErrInvalidInput
	}

	query := `DELETE FROM statements WHERE id = $1 AND tenant_id = $2`
	result, err := r.db.ExecContext(ctx, query, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete statement: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
