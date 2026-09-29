package postgres

import (
	"context"
	"database/sql"

	"stellarbill-backend/internal/metrics"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var planTracer = otel.Tracer("repository/plans")

// StartPlanSpan creates an OpenTelemetry span for plan repository operations.
func StartPlanSpan(ctx context.Context, name string, planID string) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{}
	if planID != "" {
		opts = append(opts, trace.WithAttributes(attribute.String("plan.id", planID)))
	}
	return planTracer.Start(ctx, name, opts...)
}

// MetricDBTX wraps a DBTX so every executed statement also records
// DBQueryDuration and DBQueryTotal Prometheus metrics. operation and table are
// fixed labels supplied at construction so label cardinality stays bounded by
// the caller (not by dynamic SQL text).
//
// It lives outside the sqlc-generated files (which are marked DO NOT EDIT) and
// is intended to be applied to the DBTX handed to postgres.New.
type MetricDBTX struct {
	inner     DBTX
	operation string
	table     string
}

// NewMetricDBTX returns a DBTX that records query metrics for each call with
// the given operation and table labels.
func NewMetricDBTX(inner DBTX, operation, table string) *MetricDBTX {
	return &MetricDBTX{inner: inner, operation: operation, table: table}
}

// ExecContext records query metrics and delegates.
func (m *MetricDBTX) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	done := metrics.DBTimer(m.operation, m.table)
	result, err := m.inner.ExecContext(ctx, query, args...)
	done(err)
	return result, err
}

// QueryContext records query metrics and delegates.
func (m *MetricDBTX) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	done := metrics.DBTimer(m.operation, m.table)
	rows, err := m.inner.QueryContext(ctx, query, args...)
	done(err)
	return rows, err
}

// QueryRowContext records query metrics and delegates. Row scan errors surface
// later at the caller and are not observable here; the call is timed at the
// point the row is requested.
func (m *MetricDBTX) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	done := metrics.DBTimer(m.operation, m.table)
	row := m.inner.QueryRowContext(ctx, query, args...)
	done(nil)
	return row
}

// PrepareContext records query metrics and delegates.
func (m *MetricDBTX) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	done := metrics.DBTimer(m.operation, m.table)
	stmt, err := m.inner.PrepareContext(ctx, query)
	done(err)
	return stmt, err
}
