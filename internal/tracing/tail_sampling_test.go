package tracing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTailSamplingDecisions(t *testing.T) {
	tests := []struct {
		name       string
		duration   time.Duration
		attributes []attribute.KeyValue
		recordErr  bool
		want       int
	}{
		{
			name:     "ordinary request is dropped",
			duration: 10 * time.Millisecond,
			want:     0,
		},
		{
			name:     "slow request is kept",
			duration: 100 * time.Millisecond,
			want:     1,
		},
		{
			name:       "5xx request is kept",
			duration:   10 * time.Millisecond,
			attributes: []attribute.KeyValue{attribute.Int("http.response.status_code", 503)},
			want:       1,
		},
		{
			name:       "error attribute is kept",
			duration:   10 * time.Millisecond,
			attributes: []attribute.KeyValue{attribute.String("error.type", "upstream_timeout")},
			want:       1,
		},
		{
			name:      "recorded exception is kept",
			duration:  10 * time.Millisecond,
			recordErr: true,
			want:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			cfg := testTailConfig()
			processor := newTailSpanProcessor(recorder, cfg)
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSampler(newTailSampler(sdktrace.ParentBased(sdktrace.NeverSample()))),
				sdktrace.WithSpanProcessor(processor),
			)
			t.Cleanup(func() {
				require.NoError(t, provider.Shutdown(context.Background()))
			})

			start := time.Unix(1, 0)
			_, span := provider.Tracer("test").Start(
				context.Background(),
				"request",
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithTimestamp(start),
				trace.WithAttributes(tt.attributes...),
			)
			if tt.recordErr {
				span.RecordError(errors.New("request failed"))
			}
			span.End(trace.WithTimestamp(start.Add(tt.duration)))

			assert.Len(t, recorder.Ended(), tt.want)
		})
	}
}

func TestTailSamplingPreservesBaselineDecision(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	processor := newTailSpanProcessor(recorder, testTailConfig())
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(newTailSampler(sdktrace.ParentBased(sdktrace.AlwaysSample()))),
		sdktrace.WithSpanProcessor(processor),
	)
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	_, span := provider.Tracer("test").Start(
		context.Background(),
		"ordinary-request",
		trace.WithSpanKind(trace.SpanKindServer),
	)
	span.End()

	assert.Len(t, recorder.Ended(), 1)
}

func TestTailSamplingEvictsOldestTraceDuringBurst(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	cfg := testTailConfig()
	cfg.maxTraces = 2
	processor := newTailSpanProcessor(recorder, cfg)
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(newTailSampler(sdktrace.ParentBased(sdktrace.NeverSample()))),
		sdktrace.WithSpanProcessor(processor),
	)
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	for i := byte(1); i <= 3; i++ {
		parent := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: trace.TraceID{15: i},
			SpanID:  trace.SpanID{7: i},
			Remote:  true,
		})
		ctx := trace.ContextWithRemoteSpanContext(context.Background(), parent)
		_, span := provider.Tracer("test").Start(ctx, "child")
		span.End()
	}

	processor.mu.Lock()
	assert.Len(t, processor.traces, 2)
	assert.Len(t, processor.decisions, 1)
	processor.mu.Unlock()
	assert.Empty(t, recorder.Ended())
}

func TestQualifyingRootFinishingAfterDecisionWindowIsKept(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	cfg := testTailConfig()
	cfg.decisionWindow = 10 * time.Millisecond
	processor := newTailSpanProcessor(recorder, cfg)
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(newTailSampler(sdktrace.ParentBased(sdktrace.NeverSample()))),
		sdktrace.WithSpanProcessor(processor),
	)
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})

	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{15: 1},
		SpanID:  trace.SpanID{7: 1},
		Remote:  true,
	})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), parent)
	_, child := provider.Tracer("test").Start(ctx, "early-child")
	child.End()

	processor.expire(time.Now().Add(cfg.decisionWindow), false)
	require.Empty(t, recorder.Ended())

	_, root := provider.Tracer("test").Start(
		ctx,
		"late-server-root",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.Int("http.response.status_code", 500)),
	)
	root.End()

	ended := recorder.Ended()
	require.Len(t, ended, 1)
	assert.Equal(t, "late-server-root", ended[0].Name())
}

func TestTailConfigValidation(t *testing.T) {
	t.Run("feature defaults off", func(t *testing.T) {
		t.Setenv("TRACING_TAIL_ENABLED", "")
		t.Setenv("TRACING_TAIL_LATENCY_MS", "")
		t.Setenv("TRACING_TAIL_ERROR_RATE", "")

		cfg, err := tailConfigFromEnv()

		require.NoError(t, err)
		assert.False(t, cfg.enabled)
	})

	t.Run("disabled feature ignores tail knobs", func(t *testing.T) {
		t.Setenv("TRACING_TAIL_ENABLED", "false")
		t.Setenv("TRACING_TAIL_LATENCY_MS", "invalid")
		t.Setenv("TRACING_TAIL_ERROR_RATE", "invalid")

		cfg, err := tailConfigFromEnv()

		require.NoError(t, err)
		assert.False(t, cfg.enabled)
	})

	for _, tt := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "invalid feature flag", key: "TRACING_TAIL_ENABLED", value: "perhaps"},
		{name: "zero latency", key: "TRACING_TAIL_LATENCY_MS", value: "0"},
		{name: "excessive latency", key: "TRACING_TAIL_LATENCY_MS", value: "600001"},
		{name: "negative baseline", key: "TRACING_TAIL_ERROR_RATE", value: "-0.1"},
		{name: "excessive baseline", key: "TRACING_TAIL_ERROR_RATE", value: "1.1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TRACING_TAIL_ENABLED", "true")
			t.Setenv("TRACING_TAIL_LATENCY_MS", "")
			t.Setenv("TRACING_TAIL_ERROR_RATE", "")
			if tt.key == "TRACING_TAIL_ENABLED" {
				t.Setenv("TRACING_TAIL_ENABLED", "")
			}
			t.Setenv(tt.key, tt.value)

			_, err := tailConfigFromEnv()

			assert.Error(t, err)
		})
	}
}

func testTailConfig() tailConfig {
	return tailConfig{
		enabled:        true,
		latency:        100 * time.Millisecond,
		baselineRate:   0,
		decisionWindow: time.Hour,
		maxTraces:      100,
		maxSpans:       10,
	}
}
