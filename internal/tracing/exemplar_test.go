package tracing

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestExemplarLabels_SampledRecording(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(context.Background())

	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	labels := ExemplarLabels(ctx)
	require.NotNil(t, labels, "expected non-nil labels for sampled+recording span")
	assert.Len(t, labels["trace_id"], 32, "trace_id should be 32 hex chars")
	assert.Len(t, labels["span_id"], 16, "span_id should be 16 hex chars")
}

func TestExemplarLabels_Unsampled(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	defer tp.Shutdown(context.Background())

	ctx, _ := tp.Tracer("test").Start(context.Background(), "op")
	labels := ExemplarLabels(ctx)
	assert.Nil(t, labels, "expected nil for unsampled span")
}

func TestExemplarLabels_NoSpan(t *testing.T) {
	labels := ExemplarLabels(context.Background())
	assert.Nil(t, labels, "expected nil for context without span")
}

func TestExemplarLabels_EndedSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(context.Background())

	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	span.End() // IsRecording becomes false

	labels := ExemplarLabels(ctx)
	assert.Nil(t, labels, "expected nil for ended (non-recording) span")
}

func TestExemplarLabels_InvalidSpanContext(t *testing.T) {
	// Zero trace ID — span is not valid
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{},
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
	}))
	labels := ExemplarLabels(ctx)
	assert.Nil(t, labels, "expected nil for invalid (zero TraceID) span")
}

func TestExemplarLabels_SampledButNotRecording(t *testing.T) {
	// Create a span context that is sampled but explicitly not recording
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
	}))
	// The no-op span from this context is not recording
	labels := ExemplarLabels(ctx)
	assert.Nil(t, labels, "expected nil for sampled but non-recording span")
}

func TestExemplarLabels_VerifyHexFormat(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(context.Background())

	ctx, span := tp.Tracer("test").Start(context.Background(), "hex-check")
	defer span.End()

	labels := ExemplarLabels(ctx)
	require.NotNil(t, labels)

	for _, key := range []string{"trace_id", "span_id"} {
		val := labels[key]
		require.NotEmpty(t, val, "label %q should not be empty", key)
		for _, ch := range val {
			assert.True(t,
				(ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f'),
				"label %q value %q contains non-hex char %q", key, val, string(ch))
		}
	}
}

func TestExemplarLabels_CorruptContext(t *testing.T) {
	// Context with no OTel span at all
	ctx := context.WithValue(context.Background(), "fake-key", 42)
	labels := ExemplarLabels(ctx)
	assert.Nil(t, labels, "expected nil for context without OTel span")
}

func TestExemplarLabels_ConcurrentSafety(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(context.Background())

	ctx, span := tp.Tracer("test").Start(context.Background(), "concurrent")
	defer span.End()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make(chan error, goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			labels := ExemplarLabels(ctx)
			if labels == nil {
				errs <- assert.AnError
				return
			}
			if labels["trace_id"] == "" || labels["span_id"] == "" {
				errs <- assert.AnError
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent ExemplarLabels call failed: %v", err)
	}
}

func TestExemplarLabels_ConsistencyAcrossCalls(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(context.Background())

	ctx, span := tp.Tracer("test").Start(context.Background(), "consistent")
	defer span.End()

	// Multiple calls should return identical labels
	l1 := ExemplarLabels(ctx)
	l2 := ExemplarLabels(ctx)
	require.NotNil(t, l1)
	require.NotNil(t, l2)
	assert.Equal(t, l1["trace_id"], l2["trace_id"])
	assert.Equal(t, l1["span_id"], l2["span_id"])
}

func TestExemplarLabels_TraceIDMatchesSpanContext(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer tp.Shutdown(context.Background())

	ctx, span := tp.Tracer("test").Start(context.Background(), "match-check")
	defer span.End()

	spanCtx := span.SpanContext()
	labels := ExemplarLabels(ctx)
	require.NotNil(t, labels)
	assert.Equal(t, spanCtx.TraceID().String(), labels["trace_id"])
	assert.Equal(t, spanCtx.SpanID().String(), labels["span_id"])
}

func TestExemplarLabels_NeverSampleProvider(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
	defer tp.Shutdown(context.Background())

	_, span := tp.Tracer("test").Start(context.Background(), "never-sample")
	// Even though we get a span, it should not be recording
	labels := ExemplarLabels(trace.ContextWithSpanContext(context.Background(), span.SpanContext()))
	assert.Nil(t, labels, "expected nil when provider never samples")
}
