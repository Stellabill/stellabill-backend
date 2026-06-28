package outbox

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

type recordingPublisher struct {
	called bool
}

func (p *recordingPublisher) Publish(_ context.Context, _ *Event) error {
	p.called = true
	return nil
}

func TestChaosPublisher_ProbabilityZero(t *testing.T) {
	t.Setenv("ENV", "staging")
	t.Setenv("CHAOS_OUTBOX_PROB", "0")

	inner := &recordingPublisher{}
	p := NewChaosPublisher(inner)

	err := p.Publish(context.Background(), &Event{ID: uuid.New()})
	assert.NoError(t, err)
	assert.True(t, inner.called, "inner publisher should be called when prob = 0")
}

func TestChaosPublisher_ProbabilityOne(t *testing.T) {
	t.Setenv("ENV", "staging")
	t.Setenv("CHAOS_OUTBOX_PROB", "1")

	inner := &recordingPublisher{}
	p := NewChaosPublisher(inner)

	err := p.Publish(context.Background(), &Event{ID: uuid.New()})
	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, inner.called, "inner publisher should NOT be called when chaos triggers")
}

func TestChaosPublisher_NonStagingEnv(t *testing.T) {
	t.Setenv("ENV", "development")
	t.Setenv("CHAOS_OUTBOX_PROB", "1")

	inner := &recordingPublisher{}
	p := NewChaosPublisher(inner)

	err := p.Publish(context.Background(), &Event{ID: uuid.New()})
	assert.NoError(t, err)
	assert.True(t, inner.called, "inner publisher should be called when ENV != staging")
}

func TestChaosPublisher_EnvStagingProbUnset(t *testing.T) {
	t.Setenv("ENV", "staging")

	inner := &recordingPublisher{}
	p := NewChaosPublisher(inner)

	err := p.Publish(context.Background(), &Event{ID: uuid.New()})
	assert.NoError(t, err)
	assert.True(t, inner.called, "inner publisher should be called when CHAOS_OUTBOX_PROB is unset")
}

func TestChaosPublisher_MetricIncremented(t *testing.T) {
	t.Setenv("ENV", "staging")
	t.Setenv("CHAOS_OUTBOX_PROB", "1")

	before := testutil.ToFloat64(ChaosOutboxCancellationsTotal)
	assert.Equal(t, float64(0), before, "counter should start at 0 for a fresh test binary")

	inner := &recordingPublisher{}
	p := NewChaosPublisher(inner)
	_ = p.Publish(context.Background(), &Event{ID: uuid.New()})

	after := testutil.ToFloat64(ChaosOutboxCancellationsTotal)
	assert.Equal(t, before+1, after, "counter should increment by 1 after a chaos cancellation")
}

func TestChaosPublisher_StatisticalFiftyPercent(t *testing.T) {
	t.Setenv("ENV", "staging")
	t.Setenv("CHAOS_OUTBOX_PROB", "0.5")

	inner := &recordingPublisher{}
	p := NewChaosPublisher(inner)

	var cancelled, delegated int
	const iterations = 200
	for i := 0; i < iterations; i++ {
		rec := &recordingPublisher{}
		cp := NewChaosPublisher(rec)
		err := cp.Publish(context.Background(), &Event{ID: uuid.New()})
		if err != nil {
			cancelled++
		} else {
			delegated++
		}
		_ = p // not used
	}

	assert.Greater(t, cancelled, 0, "expected at least one cancellation at prob 0.5 over %d iterations", iterations)
	assert.Greater(t, delegated, 0, "expected at least one delegation at prob 0.5 over %d iterations", iterations)
}

func TestChaosPublisher_ImplementsPublisher(t *testing.T) {
	t.Setenv("ENV", "staging")

	var _ Publisher = (*ChaosPublisher)(nil)

	inner := NewConsolePublisher()
	p := NewChaosPublisher(inner)
	assert.NotNil(t, p)
}
