package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestService() (*Service, *MockRepository) {
	repo := NewMockRepository()
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel) // suppress test output
	svc := NewService(repo, logger)
	return svc, repo
}

func TestConsume_Success(t *testing.T) {
	svc, repo := newTestService()
	raw := validRawEvent()

	event, err := svc.Consume(context.Background(), raw)
	require.NoError(t, err)
	require.NotNil(t, event)

	assert.NotEmpty(t, event.ID)
	assert.Equal(t, "key-001", event.IdempotencyKey)
	assert.Equal(t, EventContractCreated, event.EventType)
	assert.Equal(t, "contract-abc", event.ContractID)
	assert.Equal(t, "tenant-1", event.TenantID)
	assert.Equal(t, "processed", event.Status)
	assert.Equal(t, int64(1), event.SequenceNum)

	// Verify it was persisted.
	found, err := repo.FindByID(context.Background(), event.ID)
	require.NoError(t, err)
	assert.Equal(t, event.ID, found.ID)
}

func TestConsume_DuplicateEvent(t *testing.T) {
	svc, _ := newTestService()
	raw := validRawEvent()

	// First ingestion succeeds.
	_, err := svc.Consume(context.Background(), raw)
	require.NoError(t, err)

	// Second ingestion with same idempotency key is rejected.
	_, err = svc.Consume(context.Background(), raw)
	assert.ErrorIs(t, err, ErrDuplicateEvent)
}

func TestConsume_OutOfOrderEvent(t *testing.T) {
	svc, _ := newTestService()

	// Ingest event with sequence 5.
	raw := validRawEvent()
	raw.SequenceNum = 5
	raw.IdempotencyKey = "key-seq5"
	_, err := svc.Consume(context.Background(), raw)
	require.NoError(t, err)

	// Try to ingest event with sequence 3 (out of order).
	raw2 := validRawEvent()
	raw2.SequenceNum = 3
	raw2.IdempotencyKey = "key-seq3"
	_, err = svc.Consume(context.Background(), raw2)
	assert.ErrorIs(t, err, ErrOutOfOrder)
}

func TestConsume_OutOfOrder_SameSequence(t *testing.T) {
	svc, _ := newTestService()

	raw := validRawEvent()
	raw.SequenceNum = 5
	raw.IdempotencyKey = "key-first"
	_, err := svc.Consume(context.Background(), raw)
	require.NoError(t, err)

	// Same sequence number should be rejected.
	raw2 := validRawEvent()
	raw2.SequenceNum = 5
	raw2.IdempotencyKey = "key-second"
	_, err = svc.Consume(context.Background(), raw2)
	assert.ErrorIs(t, err, ErrOutOfOrder)
}

func TestConsume_SequenceZero_AlwaysAllowed(t *testing.T) {
	svc, _ := newTestService()

	// Ingest first event with seq 5.
	raw := validRawEvent()
	raw.SequenceNum = 5
	raw.IdempotencyKey = "key-seq5"
	_, err := svc.Consume(context.Background(), raw)
	require.NoError(t, err)

	// Sequence 0 bypasses ordering (unordered event).
	raw2 := validRawEvent()
	raw2.SequenceNum = 0
	raw2.IdempotencyKey = "key-seq0"
	event, err := svc.Consume(context.Background(), raw2)
	require.NoError(t, err)
	assert.NotNil(t, event)
}

func TestConsume_InOrderSequence(t *testing.T) {
	svc, _ := newTestService()

	for i := int64(1); i <= 5; i++ {
		raw := validRawEvent()
		raw.SequenceNum = i
		raw.IdempotencyKey = "key-" + string(rune('0'+i))
		_, err := svc.Consume(context.Background(), raw)
		require.NoError(t, err, "sequence %d should succeed", i)
	}
}

func TestConsume_ValidationError(t *testing.T) {
	svc, _ := newTestService()

	raw := validRawEvent()
	raw.EventType = "" // invalid
	_, err := svc.Consume(context.Background(), raw)
	assert.ErrorIs(t, err, ErrMissingEventType)
}

func TestConsume_MalformedPayload(t *testing.T) {
	svc, _ := newTestService()

	raw := validRawEvent()
	raw.Payload = json.RawMessage(`{broken}`)
	_, err := svc.Consume(context.Background(), raw)
	assert.ErrorIs(t, err, ErrInvalidPayload)
}

func TestConsume_PersistenceError(t *testing.T) {
	svc, repo := newTestService()
	repo.InsertErr = errors.New("database unavailable")

	raw := validRawEvent()
	_, err := svc.Consume(context.Background(), raw)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database unavailable")
}

func TestConsume_ReplayStorm(t *testing.T) {
	svc, _ := newTestService()
	raw := validRawEvent()

	// First succeeds.
	_, err := svc.Consume(context.Background(), raw)
	require.NoError(t, err)

	// Rapid replays all fail with duplicate error.
	for i := 0; i < 100; i++ {
		_, err := svc.Consume(context.Background(), raw)
		assert.ErrorIs(t, err, ErrDuplicateEvent)
	}
}

func TestConsume_DifferentContracts_IndependentSequences(t *testing.T) {
	svc, _ := newTestService()

	// Contract A: sequence 1.
	rawA := validRawEvent()
	rawA.ContractID = "contract-A"
	rawA.IdempotencyKey = "key-A-1"
	rawA.SequenceNum = 1
	_, err := svc.Consume(context.Background(), rawA)
	require.NoError(t, err)

	// Contract B: sequence 1 — should also succeed (independent).
	rawB := validRawEvent()
	rawB.ContractID = "contract-B"
	rawB.IdempotencyKey = "key-B-1"
	rawB.SequenceNum = 1
	_, err = svc.Consume(context.Background(), rawB)
	require.NoError(t, err)
}

func TestConsume_NilLogger(t *testing.T) {
	repo := NewMockRepository()
	svc := NewService(repo, nil) // should not panic
	raw := validRawEvent()
	event, err := svc.Consume(context.Background(), raw)
	require.NoError(t, err)
	assert.NotNil(t, event)
}

func TestConsume_ContextCancellation(t *testing.T) {
	svc, _ := newTestService()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// The mock repo doesn't respect context, so this just verifies no panic.
	raw := validRawEvent()
	_, _ = svc.Consume(ctx, raw)
}

