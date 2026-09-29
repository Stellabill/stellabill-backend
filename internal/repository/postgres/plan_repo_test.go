package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"stellarbill-backend/internal/metrics"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// stubDBTx is a minimal DBTX double that returns fixed results.
type stubDBTx struct {
	execResult sql.Result
	execErr    error
	rows       *sql.Rows
	rowsErr    error
	row        *sql.Row
	stmt       *sql.Stmt
	stmtErr    error

	execCalls    int
	queryCalls   int
	queryRowCall int
	prepareCalls int
}

func (s *stubDBTx) ExecContext(context.Context, string, ...interface{}) (sql.Result, error) {
	s.execCalls++
	return s.execResult, s.execErr
}

func (s *stubDBTx) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	s.prepareCalls++
	return s.stmt, s.stmtErr
}

func (s *stubDBTx) QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error) {
	s.queryCalls++
	return s.rows, s.rowsErr
}

func (s *stubDBTx) QueryRowContext(context.Context, string, ...interface{}) *sql.Row {
	s.queryRowCall++
	return s.row
}

func resetDBMetrics() {
	metrics.DBQueryDuration.Reset()
	metrics.DBQueryTotal.Reset()
}

func TestMetricDBTX_RecordsDurationAndTotal(t *testing.T) {
	resetDBMetrics()

	selectWrapper := NewMetricDBTX(&stubDBTx{}, "SELECT", "plans")
	updateWrapper := NewMetricDBTX(&stubDBTx{}, "UPDATE", "plans")

	if _, err := selectWrapper.QueryContext(context.Background(), "SELECT id FROM plans"); err != nil {
		t.Fatalf("QueryContext: %v", err)
	}
	if _, err := updateWrapper.ExecContext(context.Background(), "UPDATE plans SET name=$1"); err != nil {
		t.Fatalf("ExecContext: %v", err)
	}

	if got := testutil.ToFloat64(metrics.DBQueryTotal.WithLabelValues("SELECT", "plans", "false")); got != 1 {
		t.Errorf("expected SELECT counter 1, got %v", got)
	}
	if got := testutil.ToFloat64(metrics.DBQueryTotal.WithLabelValues("UPDATE", "plans", "false")); got != 1 {
		t.Errorf("expected UPDATE counter 1, got %v", got)
	}
	if testutil.CollectAndCount(metrics.DBQueryDuration) == 0 {
		t.Error("expected DBQueryDuration observations")
	}
}

func TestMetricDBTX_RecordsErrorStatus(t *testing.T) {
	resetDBMetrics()

	inner := &stubDBTx{execErr: errors.New("boom")}
	m := NewMetricDBTX(inner, "INSERT", "plans")

	if _, err := m.ExecContext(context.Background(), "INSERT INTO plans"); err == nil {
		t.Fatal("expected error to propagate")
	}

	if got := testutil.ToFloat64(metrics.DBQueryTotal.WithLabelValues("INSERT", "plans", "true")); got != 1 {
		t.Errorf("expected INSERT error counter 1, got %v", got)
	}
}

func TestMetricDBTX_DelegatesToInner(t *testing.T) {
	inner := &stubDBTx{}
	m := NewMetricDBTX(inner, "SELECT", "plans")

	ctx := context.Background()
	_, _ = m.QueryContext(ctx, "q")
	_, _ = m.ExecContext(ctx, "e")
	_ = m.QueryRowContext(ctx, "r")
	_, _ = m.PrepareContext(ctx, "p")

	if inner.queryCalls != 1 || inner.execCalls != 1 || inner.queryRowCall != 1 || inner.prepareCalls != 1 {
		t.Fatalf("expected delegation to inner DBTX, got exec=%d query=%d queryRow=%d prepare=%d",
			inner.execCalls, inner.queryCalls, inner.queryRowCall, inner.prepareCalls)
	}
}

func TestNewMetricDBTX_ReturnsType(t *testing.T) {
	mt := NewMetricDBTX(&stubDBTx{}, "SELECT", "plans")
	if mt == nil || mt.operation != "SELECT" || mt.table != "plans" {
		t.Fatal("NewMetricDBTX did not set operation/table labels")
	}
	if mt.inner == nil {
		t.Fatal("NewMetricDBTX did not retain inner DBTX")
	}
}
