package tracing

import (
	"context"
	"math"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestNewTenantAwareSampler(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.05)
	require.NotNil(t, s)
	assert.Contains(t, s.Description(), "TenantAware")
}

func TestTenantAwareSampler_ClampsRatios(t *testing.T) {
	t.Run("negative clamped to zero", func(t *testing.T) {
		s := NewTenantAwareSampler(-0.5, -0.5, -0.5)
		params := samplingParams("free")
		result := s.ShouldSample(params)
		assert.Equal(t, sdktrace.Drop, result.Decision)
	})

	t.Run("above-one clamped to one", func(t *testing.T) {
		s := NewTenantAwareSampler(2.0, 2.0, 2.0)
		params := samplingParams("free")
		result := s.ShouldSample(params)
		assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
	})
}

func TestTenantAwareSampler_EnterpriseAlwaysSamples(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.05)
	for _, tid := range []trace.TraceID{{1}, {2}, {255}, {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255}} {
		params := sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			TraceID:       tid,
			Attributes:    []attribute.KeyValue{attribute.String("tier", "enterprise")},
		}
		result := s.ShouldSample(params)
		assert.Equal(t, sdktrace.RecordAndSample, result.Decision, "trace %v should be sampled for enterprise", tid)
	}
}

func TestTenantAwareSampler_FreeProbabilisticSampling(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.05)
	var sampled, dropped int
	n := 10000
	for i := range n {
		tid := trace.TraceID{byte(i >> 8), byte(i)}
		params := sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			TraceID:       tid,
			Attributes:    []attribute.KeyValue{attribute.String("tier", "free")},
		}
		result := s.ShouldSample(params)
		if result.Decision == sdktrace.RecordAndSample {
			sampled++
		} else {
			dropped++
		}
	}
	assert.Greater(t, sampled, 0, "expected at least one free-tier sample")
	assert.Greater(t, dropped, 0, "expected at least one free-tier drop")
	// ~1% of 10000 = 100; allow generous margin
	ratio := float64(sampled) / float64(n)
	assert.Less(t, math.Abs(ratio-0.01), 0.02, "free ratio should be near 0.01")
}

func TestTenantAwareSampler_DefaultRatioUsedWhenNoTier(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.5) // default=0.5
	var sampled, dropped int
	n := 1000
	for i := range n {
		tid := trace.TraceID{byte(i >> 8), byte(i)}
		params := sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			TraceID:       tid,
		}
		result := s.ShouldSample(params)
		if result.Decision == sdktrace.RecordAndSample {
			sampled++
		} else {
			dropped++
		}
	}
	assert.Greater(t, sampled, 0, "expected at least one default sample at 0.5 ratio")
	assert.Greater(t, dropped, 0, "expected at least one default drop at 0.5 ratio")
	ratio := float64(sampled) / float64(n)
	assert.Less(t, math.Abs(ratio-0.5), 0.1, "default ratio should be near 0.5")
}

func TestTenantAwareSampler_UnknownTierUsesDefault(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.0)
	params := sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		TraceID:       trace.TraceID{1},
		Attributes:    []attribute.KeyValue{attribute.String("tier", "nonexistent")},
	}
	result := s.ShouldSample(params)
	assert.Equal(t, sdktrace.Drop, result.Decision)
}

func TestTenantAwareSampler_TierFromBaggage(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.0)
	m, _ := baggage.NewMember("tier", "enterprise")
	bag, _ := baggage.New(m)
	ctx := baggage.ContextWithBaggage(context.Background(), bag)

	params := sdktrace.SamplingParameters{
		ParentContext: ctx,
		TraceID:       trace.TraceID{42},
	}
	result := s.ShouldSample(params)
	assert.Equal(t, sdktrace.RecordAndSample, result.Decision,
		"enterprise tier from baggage should be sampled")
}

func TestTenantAwareSampler_BaggageOverriddenByAttribute(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.0, 0.5) // free ratio=0 → always drop
	m, _ := baggage.NewMember("tier", "enterprise")
	bag, _ := baggage.New(m)
	ctx := baggage.ContextWithBaggage(context.Background(), bag)

	// Attribute should take precedence over baggage
	params := sdktrace.SamplingParameters{
		ParentContext: ctx,
		TraceID:       trace.TraceID{99},
		Attributes:    []attribute.KeyValue{attribute.String("tier", "free")},
	}
	result := s.ShouldSample(params)
	assert.Equal(t, sdktrace.Drop, result.Decision,
		"attribute should override baggage; free with 0 ratio should drop")
}

func TestTenantAwareSampler_ParentBasedInheritance(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.0)

	// Simulate a parent that was sampled
	sampledCtx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1},
		SpanID:  trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
	}))

	childParams := sdktrace.SamplingParameters{
		ParentContext: sampledCtx,
		TraceID:       trace.TraceID{1},
	}
	result := s.ShouldSample(childParams)
	assert.Equal(t, sdktrace.RecordAndSample, result.Decision,
		"child of sampled parent should be sampled regardless of tier")
}

func TestTenantAwareSampler_ParentBasedDropInheritance(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.0)

	// Simulate a parent that was NOT sampled
	droppedCtx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{2},
		SpanID:     trace.SpanID{2},
		TraceFlags: 0,
	}))

	childParams := sdktrace.SamplingParameters{
		ParentContext: droppedCtx,
		TraceID:       trace.TraceID{2},
	}
	result := s.ShouldSample(childParams)
	assert.Equal(t, sdktrace.Drop, result.Decision,
		"child of dropped parent should be dropped regardless of tier")
}

func TestTenantAwareSampler_MetricsIncremented(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.05)

	before := testutil.ToFloat64(tracesSampledTotal.WithLabelValues("enterprise"))

	params := samplingParams("enterprise")
	s.ShouldSample(params)

	after := testutil.ToFloat64(tracesSampledTotal.WithLabelValues("enterprise"))
	assert.Equal(t, before+1, after, "enterprise counter should increment by 1")
}

func TestTenantAwareSampler_MetricsPerTier(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.05)

	beforeFree := testutil.ToFloat64(tracesSampledTotal.WithLabelValues("free"))
	beforeEnt := testutil.ToFloat64(tracesSampledTotal.WithLabelValues("enterprise"))
	beforeUnk := testutil.ToFloat64(tracesSampledTotal.WithLabelValues("unknown"))

	s.ShouldSample(samplingParams("free"))
	s.ShouldSample(samplingParams("enterprise"))
	s.ShouldSample(samplingParams(""))

	assert.Equal(t, beforeFree+1, testutil.ToFloat64(tracesSampledTotal.WithLabelValues("free")))
	assert.Equal(t, beforeEnt+1, testutil.ToFloat64(tracesSampledTotal.WithLabelValues("enterprise")))
	assert.Equal(t, beforeUnk+1, testutil.ToFloat64(tracesSampledTotal.WithLabelValues("unknown")))
}

func TestTenantAwareSampler_Description(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.05)
	desc := s.Description()
	assert.Contains(t, desc, "TenantAware")
	assert.Contains(t, desc, "AlwaysSample")
	assert.Contains(t, desc, "TraceIDRatioBased")
}

func TestExtractTier(t *testing.T) {
	t.Run("from attributes", func(t *testing.T) {
		params := sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			Attributes:    []attribute.KeyValue{attribute.String("tier", "enterprise")},
		}
		assert.Equal(t, "enterprise", extractTier(params))
	})

	t.Run("from baggage fallback", func(t *testing.T) {
		m, _ := baggage.NewMember("tier", "free")
		bag, _ := baggage.New(m)
		ctx := baggage.ContextWithBaggage(context.Background(), bag)
		params := sdktrace.SamplingParameters{
			ParentContext: ctx,
		}
		assert.Equal(t, "free", extractTier(params))
	})

	t.Run("unknown when missing", func(t *testing.T) {
		params := sdktrace.SamplingParameters{
			ParentContext: context.Background(),
		}
		assert.Equal(t, "unknown", extractTier(params))
	})

	t.Run("empty attributes", func(t *testing.T) {
		params := sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			Attributes:    []attribute.KeyValue{},
		}
		assert.Equal(t, "unknown", extractTier(params))
	})
}

func TestGetEnvFloat(t *testing.T) {
	t.Run("valid value", func(t *testing.T) {
		t.Setenv("TEST_FLOAT", "0.75")
		assert.Equal(t, 0.75, getEnvFloat("TEST_FLOAT", 0.5))
	})

	t.Run("missing key returns fallback", func(t *testing.T) {
		assert.Equal(t, 0.5, getEnvFloat("TEST_FLOAT_NONEXISTENT", 0.5))
	})

	t.Run("invalid value returns fallback", func(t *testing.T) {
		t.Setenv("TEST_FLOAT", "not-a-float")
		assert.Equal(t, 0.5, getEnvFloat("TEST_FLOAT", 0.5))
	})

	t.Run("negative value returns fallback", func(t *testing.T) {
		t.Setenv("TEST_FLOAT", "-0.5")
		assert.Equal(t, 0.5, getEnvFloat("TEST_FLOAT", 0.5))
	})

	t.Run("value above 1 returns fallback", func(t *testing.T) {
		t.Setenv("TEST_FLOAT", "1.5")
		assert.Equal(t, 0.5, getEnvFloat("TEST_FLOAT", 0.5))
	})

	t.Run("exactly zero is valid", func(t *testing.T) {
		t.Setenv("TEST_FLOAT", "0")
		assert.Equal(t, 0.0, getEnvFloat("TEST_FLOAT", 0.5))
	})

	t.Run("exactly one is valid", func(t *testing.T) {
		t.Setenv("TEST_FLOAT", "1")
		assert.Equal(t, 1.0, getEnvFloat("TEST_FLOAT", 0.5))
	})
}

func TestInitTracer(t *testing.T) {
	shutdown, err := InitTracer("test-service")
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown())
}

func TestInitTracer_EnvRatios(t *testing.T) {
	t.Setenv("TRACING_ENTERPRISE_RATIO", "0.5")
	t.Setenv("TRACING_FREE_RATIO", "0.25")
	t.Setenv("TRACING_DEFAULT_RATIO", "0.1")

	shutdown, err := InitTracer("test-service")
	require.NoError(t, err)
	defer shutdown()
}

func TestTenantAwareSampler_AllTiersRecordMetric(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.05)

	tiers := []string{"enterprise", "free", "unknown", "premium"}
	counts := make(map[string]float64)
	for _, tier := range tiers {
		counts[tier] = testutil.ToFloat64(tracesSampledTotal.WithLabelValues(tier))
	}

	for _, tier := range tiers {
		params := samplingParams(tier)
		s.ShouldSample(params)
	}

	for _, tier := range tiers {
		expected := counts[tier] + 1
		got := testutil.ToFloat64(tracesSampledTotal.WithLabelValues(tier))
		assert.Equal(t, expected, got, "metric for tier %q should increment", tier)
	}
}

func samplingParams(tier string) sdktrace.SamplingParameters {
	attrs := []attribute.KeyValue{}
	if tier != "" {
		attrs = append(attrs, attribute.String("tier", tier))
	}
	return sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		TraceID:       trace.TraceID{1},
		Attributes:    attrs,
	}
}
