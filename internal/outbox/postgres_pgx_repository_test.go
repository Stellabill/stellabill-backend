package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"stellarbill-backend/internal/db"
)

type mockPgxPool struct {
	execFn     func(ctx context.Context, sql string, args ...any) (pgx.CommandTag, error)
	copyFromFn func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	beginFn    func(ctx context.Context) (pgx.Tx, error)
	beginTxFn  func(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func (m *mockPgxPool) Exec(ctx context.Context, sql string, args ...any) (pgx.CommandTag, error) {
	if m.execFn != nil {
		return m.execFn(ctx, sql, args...)
	}
	return pgx.CommandTag("INSERT 0 1"), nil
}

func (m *mockPgxPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return nil
}

func (m *mockPgxPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return nil, nil
}

func (m *mockPgxPool) Begin(ctx context.Context) (pgx.Tx, error) {
	if m.beginFn != nil {
		return m.beginFn(ctx)
	}
	return nil, nil
}

func (m *mockPgxPool) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	if m.beginTxFn != nil {
		return m.beginTxFn(ctx, txOptions)
	}
	return nil, nil
}

func (m *mockPgxPool) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	if m.copyFromFn != nil {
		return m.copyFromFn(ctx, tableName, columnNames, rowSrc)
	}
	return 0, nil
}

func makeTestEvent() *Event {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return &Event{
		ID:            uuid.New(),
		TenantID:      "tenant-1",
		EventType:     "test.event",
		EventData:     json.RawMessage(`{"type":"test.event"}`),
		AggregateID:   strPtr("agg-1"),
		AggregateType: strPtr("aggregate"),
		OccurredAt:    now,
		Status:        StatusPending,
		RetryCount:    0,
		MaxRetries:    3,
		CreatedAt:     now,
		UpdatedAt:     now,
		Version:       1,
	}
}

func strPtr(s string) *string { return &s }

func TestPostgresPgxRepository_BulkInsert_Empty(t *testing.T) {
	pool := &mockPgxPool{}
	repo := &PostgresPgxRepository{pool: pool}

	err := repo.BulkInsert(context.Background(), nil)
	assert.NoError(t, err)

	err = repo.BulkInsert(context.Background(), []*Event{})
	assert.NoError(t, err)
}

func TestPostgresPgxRepository_BulkInsert_SingleEvent(t *testing.T) {
	var executedSQL string
	var executedArgs []any
	pool := &mockPgxPool{
		execFn: func(ctx context.Context, sql string, args ...any) (pgx.CommandTag, error) {
			executedSQL = sql
			executedArgs = args
			return pgx.CommandTag("INSERT 0 1"), nil
		},
	}
	repo := &PostgresPgxRepository{pool: pool}
	event := makeTestEvent()

	err := repo.BulkInsert(context.Background(), []*Event{event})
	require.NoError(t, err)
	assert.Contains(t, executedSQL, "INSERT INTO outbox_events")
	assert.Equal(t, event.ID, executedArgs[0])
}

func TestPostgresPgxRepository_BulkInsert_MultipleEvents(t *testing.T) {
	var capturedTable pgx.Identifier
	var capturedColumns []string
	var capturedRows [][]any

	pool := &mockPgxPool{
		copyFromFn: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
			capturedTable = tableName
			capturedColumns = columnNames
			capturedRows = nil
			for rowSrc.Next() {
				values, err := rowSrc.Values()
				if err != nil {
					return 0, err
				}
				capturedRows = append(capturedRows, values)
			}
			return int64(len(capturedRows)), nil
		},
	}
	repo := &PostgresPgxRepository{pool: pool}

	events := []*Event{makeTestEvent(), makeTestEvent(), makeTestEvent()}
	err := repo.BulkInsert(context.Background(), events)
	require.NoError(t, err)

	assert.Equal(t, pgx.Identifier{"outbox_events"}, capturedTable)
	expectedColumns := []string{
		"id", "tenant_id", "event_type", "event_data", "aggregate_id", "aggregate_type",
		"occurred_at", "status", "retry_count", "max_retries", "next_retry_at",
		"error_message", "created_at", "updated_at", "version", "deduplication_id",
	}
	assert.Equal(t, expectedColumns, capturedColumns)
	assert.Len(t, capturedRows, 3)

	for i, row := range capturedRows {
		assert.Equal(t, events[i].ID, row[0])
		assert.Equal(t, events[i].TenantID, row[1])
		assert.Equal(t, events[i].EventType, row[2])
	}
}

func TestPostgresPgxRepository_BulkInsert_CopyFromError(t *testing.T) {
	pool := &mockPgxPool{
		copyFromFn: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
			return 0, errors.New("copy error")
		},
	}
	repo := &PostgresPgxRepository{pool: pool}
	events := []*Event{makeTestEvent(), makeTestEvent()}

	err := repo.BulkInsert(context.Background(), events)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copy error")
}

func TestPostgresPgxRepository_BulkInsert_AllFieldsPreserved(t *testing.T) {
	var capturedRows [][]any
	pool := &mockPgxPool{
		copyFromFn: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
			capturedRows = nil
			for rowSrc.Next() {
				values, err := rowSrc.Values()
				if err != nil {
					return 0, err
				}
				capturedRows = append(capturedRows, values)
			}
			return int64(len(capturedRows)), nil
		},
	}
	repo := &PostgresPgxRepository{pool: pool}

	now := time.Now().UTC().Truncate(time.Microsecond)
	nextRetry := now.Add(time.Hour)
	errMsg := "test error"
	dedupID := "dedup-1"

	event := &Event{
		ID:              uuid.New(),
		TenantID:        "tenant-2",
		EventType:       "custom.event",
		EventData:       json.RawMessage(`{"key":"value"}`),
		AggregateID:     strPtr("agg-2"),
		AggregateType:   strPtr("custom"),
		OccurredAt:      now,
		Status:          StatusFailed,
		RetryCount:      2,
		MaxRetries:      5,
		NextRetryAt:     &nextRetry,
		ErrorMessage:    &errMsg,
		CreatedAt:       now,
		UpdatedAt:       now,
		Version:         3,
		DeduplicationID: &dedupID,
	}

	err := repo.BulkInsert(context.Background(), []*Event{event, makeTestEvent()})
	require.NoError(t, err)
	require.Len(t, capturedRows, 2)

	row := capturedRows[0]
	assert.Equal(t, event.ID, row[0])
	assert.Equal(t, event.TenantID, row[1])
	assert.Equal(t, event.EventType, row[2])
	assert.Equal(t, event.EventData, row[3])
	assert.Equal(t, event.AggregateID, row[4])
	assert.Equal(t, event.AggregateType, row[5])
	assert.Equal(t, event.OccurredAt, row[6])
	assert.Equal(t, event.Status, row[7])
	assert.Equal(t, event.RetryCount, row[8])
	assert.Equal(t, event.MaxRetries, row[9])
	assert.Equal(t, event.NextRetryAt, row[10])
	assert.Equal(t, event.ErrorMessage, row[11])
	assert.Equal(t, event.CreatedAt, row[12])
	assert.Equal(t, event.UpdatedAt, row[13])
	assert.Equal(t, event.Version, row[14])
	assert.Equal(t, event.DeduplicationID, row[15])
}

func TestPostgresPgxRepository_BulkInsert_ContextCancelled(t *testing.T) {
	pool := &mockPgxPool{
		copyFromFn: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
			return 0, context.Canceled
		},
	}
	repo := &PostgresPgxRepository{pool: pool}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := repo.BulkInsert(ctx, []*Event{makeTestEvent(), makeTestEvent()})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func BenchmarkPostgresPgxRepository_BulkInsert_1(b *testing.B) {
	benchmarkBulkInsert(b, 1)
}

func BenchmarkPostgresPgxRepository_BulkInsert_10(b *testing.B) {
	benchmarkBulkInsert(b, 10)
}

func BenchmarkPostgresPgxRepository_BulkInsert_100(b *testing.B) {
	benchmarkBulkInsert(b, 100)
}

func BenchmarkPostgresPgxRepository_BulkInsert_1000(b *testing.B) {
	benchmarkBulkInsert(b, 1000)
}

func benchmarkBulkInsert(b *testing.B, n int) {
	var capturedRows int64
	pool := &mockPgxPool{
		copyFromFn: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
			var count int64
			for rowSrc.Next() {
				rowSrc.Values()
				count++
			}
			return count, nil
		},
	}
	repo := &PostgresPgxRepository{pool: pool}

	events := make([]*Event, n)
	for i := range events {
		events[i] = makeTestEvent()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		capturedRows = 0
		err := repo.BulkInsert(context.Background(), events)
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = capturedRows
}

func TestPostgresPgxRepository_BulkInsert_SameTenantSetsContext(t *testing.T) {
	var capturedCtx context.Context
	pool := &mockPgxPool{
		copyFromFn: func(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
			capturedCtx = ctx
			return 2, nil
		},
	}
	repo := &PostgresPgxRepository{pool: pool}

	e1 := makeTestEvent()
	e2 := makeTestEvent()
	e2.TenantID = e1.TenantID

	err := repo.BulkInsert(context.Background(), []*Event{e1, e2})
	require.NoError(t, err)

	tenantID, err := db.TenantIDFromContext(capturedCtx)
	require.NoError(t, err)
	assert.Equal(t, e1.TenantID, tenantID)
}
