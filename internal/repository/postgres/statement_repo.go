package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"stellarbill-backend/internal/repository"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var stmtTracer = otel.Tracer("repository/postgres")

// StatementRepo implements repository.StatementRepository against a live Postgres database.
type StatementRepo struct {
	pool *pgxpool.Pool
}

// NewStatementRepo constructs a StatementRepo using the provided connection pool.
func NewStatementRepo(pool *pgxpool.Pool) *StatementRepo {
	return &StatementRepo{pool: pool}
}

// FindByID fetches the statement with the given ID.
func (r *StatementRepo) FindByID(ctx context.Context, id string) (*repository.StatementRow, error) {
	const q = `
		SELECT id, subscription_id, customer_id, period_start, period_end, issued_at, total_amount, currency, kind, status, deleted_at
		FROM statements
		WHERE id = $1`

	var s repository.StatementRow
	ctx, span := stmtTracer.Start(ctx, "StatementRepo.FindByID",
		trace.WithAttributes(attribute.String("statement.id", id)))
	defer span.End()

	err := r.pool.QueryRow(ctx, q, id).Scan(
		&s.ID, &s.SubscriptionID, &s.CustomerID, &s.PeriodStart, &s.PeriodEnd,
		&s.IssuedAt, &s.TotalAmount, &s.Currency, &s.Kind, &s.Status, &s.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

// ListByCustomerID retrieves a list of statements for a customer with deterministic ordering and cursor pagination.
func (r *StatementRepo) ListByCustomerID(ctx context.Context, customerID string, q repository.StatementQuery) ([]*repository.StatementRow, int, error) {
	ctx, span := stmtTracer.Start(ctx, "StatementRepo.ListByCustomerID",
		trace.WithAttributes(attribute.String("customer.id", customerID)))
	defer span.End()

	var args []interface{}
	argCount := 1
	args = append(args, customerID)

	whereClauses := []string{"customer_id = $1", "deleted_at IS NULL"}

	if q.SubscriptionID != "" {
		argCount++
		whereClauses = append(whereClauses, fmt.Sprintf("subscription_id = $%d", argCount))
		args = append(args, q.SubscriptionID)
	}
	if q.Kind != "" {
		argCount++
		whereClauses = append(whereClauses, fmt.Sprintf("kind = $%d", argCount))
		args = append(args, q.Kind)
	}
	if q.Status != "" {
		argCount++
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", argCount))
		args = append(args, q.Status)
	}

	// Deterministic ordering: issued_at DESC, id DESC (default)
	orderDir := "DESC"
	if strings.ToLower(q.Order) == "asc" {
		orderDir = "ASC"
	}
	orderBy := fmt.Sprintf("issued_at %s, id %s", orderDir, orderDir)

	// Safe cursor-based pagination
	if q.StartingAfter != "" {
		// Fetch the pivot row to get its issued_at
		var pivotIssuedAt string
		err := r.pool.QueryRow(ctx, "SELECT issued_at FROM statements WHERE id = $1", q.StartingAfter).Scan(&pivotIssuedAt)
		if err == nil {
			// Deterministic tie-breaking: (issued_at, id) < (pivot_issued_at, pivot_id) for DESC
			op := "<"
			if orderDir == "ASC" {
				op = ">"
			}
			argCount++
			whereClauses = append(whereClauses, fmt.Sprintf("(issued_at, id) %s ($%d, $%d)", op, argCount, argCount+1))
			args = append(args, pivotIssuedAt, q.StartingAfter)
			argCount++
		}
	} else if q.EndingBefore != "" {
		// Backward pagination logic
		var pivotIssuedAt string
		err := r.pool.QueryRow(ctx, "SELECT issued_at FROM statements WHERE id = $1", q.EndingBefore).Scan(&pivotIssuedAt)
		if err == nil {
			op := ">"
			if orderDir == "ASC" {
				op = "<"
			}
			argCount++
			whereClauses = append(whereClauses, fmt.Sprintf("(issued_at, id) %s ($%d, $%d)", op, argCount, argCount+1))
			args = append(args, pivotIssuedAt, q.EndingBefore)
			argCount++
			// Reverse order to get the items immediately BEFORE the pivot
			revDir := "ASC"
			if orderDir == "ASC" {
				revDir = "DESC"
			}
			orderBy = fmt.Sprintf("issued_at %s, id %s", revDir, revDir)
		}
	}

	where := strings.Join(whereClauses, " AND ")
	
	// Count total (regardless of pagination)
	countQuery := "SELECT COUNT(*) FROM statements WHERE customer_id = $1 AND deleted_at IS NULL"
	var total int
	err := r.pool.QueryRow(ctx, countQuery, customerID).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Fetch rows
	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}
	
	fetchQuery := fmt.Sprintf(`
		SELECT id, subscription_id, customer_id, period_start, period_end, issued_at, total_amount, currency, kind, status, deleted_at
		FROM statements
		WHERE %s
		ORDER BY %s
		LIMIT %d`, where, orderBy, limit)

	rows, err := r.pool.Query(ctx, fetchQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []*repository.StatementRow
	for rows.Next() {
		var s repository.StatementRow
		err := rows.Scan(
			&s.ID, &s.SubscriptionID, &s.CustomerID, &s.PeriodStart, &s.PeriodEnd,
			&s.IssuedAt, &s.TotalAmount, &s.Currency, &s.Kind, &s.Status, &s.DeletedAt,
		)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, &s)
	}

	// If it was backward pagination, we need to reverse the result back to requested order
	if q.EndingBefore != "" {
		for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
			result[i], result[j] = result[j], result[i]
		}
	}

	return result, total, nil
}
