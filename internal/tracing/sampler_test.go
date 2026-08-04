package tracing

import (
	"context"
	"encoding/binary"
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
	assert.NotNil(t, s.enterprise)
	assert.NotNil(t, s.free)
	assert.NotNil(t, s.defaultS)
}

func TestTenantAwareSampler_ClampsRatios(t *testing.T) {
	t.Run("negative clamped to zero", func(t *testing.T) {
		s := NewTenantAwareSampler(-0.5, -0.1, -1.0)
		params := samplingParams("enterprise")
		result := s.ShouldSample(params)
		assert.Equal(t, sdktrace.Drop, result.Decision)
	})

	t.Run("above one clamped to one", func(t *testing.T) {
		s := NewTenantAwareSampler(1.5, 2.0, 5.0)
		params := samplingParams("free")
		result := s.ShouldSample(params)
		assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
	})
}

func TestTenantAwareSampler_EnterpriseAlwaysSamples(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.05)
	for i := range 100 {
		params := sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			TraceID:       trace.TraceID{byte(i)},
			Attributes:    []attribute.KeyValue{attribute.String("tier", "enterprise")},
		}
		result := s.ShouldSample(params)
		assert.Equal(t, sdktrace.RecordAndSample, result.Decision)
	}
}

func TestTenantAwareSampler_FreeProbabilisticSampling(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.05)
	var sampled, dropped int
	n := 10000
	step := math.MaxUint64 / uint64(n)
	for i := range n {
		var tid trace.TraceID
		binary.BigEndian.PutUint64(tid[8:], uint64(i)*step)
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
	ratio := float64(sampled) / float64(n)
	assert.Less(t, math.Abs(ratio-0.01), 0.02, "free ratio should be near 0.01")
}

func TestTenantAwareSampler_DefaultRatioUsedWhenNoTier(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.5)
	var sampled, dropped int
	n := 1000
	step := math.MaxUint64 / uint64(n)
	for i := range n {
		var tid trace.TraceID
		binary.BigEndian.PutUint64(tid[8:], uint64(i)*step)
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
	s := NewTenantAwareSampler(1.0, 0.0, 0.5)
	m, _ := baggage.NewMember("tier", "enterprise")
	bag, _ := baggage.New(m)
	ctx := baggage.ContextWithBaggage(context.Background(), bag)

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

	sampledCtx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{1},
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

func TestTenantAwareSampler_MetricsIncrementedOnlyOnSample(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.0, 0.0)

	beforeFree := testutil.ToFloat64(tracesSampledTotal.WithLabelValues("free"))
	beforeEnt := testutil.ToFloat64(tracesSampledTotal.WithLabelValues("enterprise"))

	paramsFree := samplingParams("free")
	resFree := s.ShouldSample(paramsFree)
	assert.Equal(t, sdktrace.Drop, resFree.Decision)
	assert.Equal(t, beforeFree, testutil.ToFloat64(tracesSampledTotal.WithLabelValues("free")),
		"dropped trace must not increment metric counter")

	paramsEnt := samplingParams("enterprise")
	resEnt := s.ShouldSample(paramsEnt)
	assert.Equal(t, sdktrace.RecordAndSample, resEnt.Decision)
	assert.Equal(t, beforeEnt+1, testutil.ToFloat64(tracesSampledTotal.WithLabelValues("enterprise")),
		"sampled trace must increment metric counter")
}

func TestExtractTier_KeyVariants(t *testing.T) {
	t.Run("attribute tenant_tier", func(t *testing.T) {
		params := sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			Attributes:    []attribute.KeyValue{attribute.String("tenant_tier", "enterprise")},
		}
		assert.Equal(t, "enterprise", extractTier(params))
	})

	t.Run("attribute tenant.tier", func(t *testing.T) {
		params := sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			Attributes:    []attribute.KeyValue{attribute.String("tenant.tier", "free")},
		}
		assert.Equal(t, "free", extractTier(params))
	})

	t.Run("baggage tenant_tier fallback", func(t *testing.T) {
		m, _ := baggage.NewMember("tenant_tier", "enterprise")
		bag, _ := baggage.New(m)
		ctx := baggage.ContextWithBaggage(context.Background(), bag)
		params := sdktrace.SamplingParameters{
			ParentContext: ctx,
		}
		assert.Equal(t, "enterprise", extractTier(params))
	})

	t.Run("baggage tenant.tier fallback", func(t *testing.T) {
		m, _ := baggage.NewMember("tenant.tier", "free")
		bag, _ := baggage.New(m)
		ctx := baggage.ContextWithBaggage(context.Background(), bag)
		params := sdktrace.SamplingParameters{
			ParentContext: ctx,
		}
		assert.Equal(t, "free", extractTier(params))
	})
}

func TestTenantAwareSampler_Description(t *testing.T) {
	s := NewTenantAwareSampler(1.0, 0.01, 0.05)
	desc := s.Description()
	assert.Contains(t, desc, "TenantAware")
	assert.Contains(t, desc, "TraceIDRatioBased")
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

func TestBaggageSpanProcessor_Coverage(t *testing.T) {
	bsp := BaggageSpanProcessor{}
	bsp.OnEnd(nil)
	assert.Nil(t, bsp.Shutdown(context.Background()))
	assert.Nil(t, bsp.ForceFlush(context.Background()))
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
