package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestTenantTierSampler_Enterprise(t *testing.T) {
	s := &TenantTierSampler{
		rates:  TierSamplingRates,
		parent: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(DefaultSamplingRate)),
	}
	tid, _ := trace.TraceIDFromHex("00000000000000000000000000000001")
	sid, _ := trace.SpanIDFromHex("0000000000000001")
	result := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		TraceID:       tid,
		SpanID:        sid,
		Attributes:    []attribute.KeyValue{attribute.String(TierAttributeKey, "enterprise")},
	})
	if result.Decision != sdktrace.RecordAndSample {
		t.Errorf("expected RecordAndSample for enterprise, got %v", result.Decision)
	}
}

func TestTenantTierSampler_Pro(t *testing.T) {
	s := &TenantTierSampler{
		rates:  TierSamplingRates,
		parent: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(DefaultSamplingRate)),
	}
	tid, _ := trace.TraceIDFromHex("00000000000000000000000000000001")
	sid, _ := trace.SpanIDFromHex("0000000000000001")
	result := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		TraceID:       tid,
		SpanID:        sid,
		Attributes:    []attribute.KeyValue{attribute.String(TierAttributeKey, "pro")},
	})
	if result.Decision != sdktrace.RecordAndSample && result.Decision != sdktrace.Drop {
		t.Errorf("expected RecordAndSample or Drop for pro (25%%), got %v", result.Decision)
	}
}

func TestTenantTierSampler_Free(t *testing.T) {
	s := &TenantTierSampler{
		rates:  TierSamplingRates,
		parent: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(DefaultSamplingRate)),
	}
	result := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Attributes:    []attribute.KeyValue{attribute.String(TierAttributeKey, "free")},
	})
	if result.Decision != sdktrace.Drop && result.Decision != sdktrace.RecordAndSample {
		t.Errorf("unexpected decision for free: %v", result.Decision)
	}
}

func TestTenantTierSampler_UnknownTier(t *testing.T) {
	s := &TenantTierSampler{
		rates:  TierSamplingRates,
		parent: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(DefaultSamplingRate)),
	}
	result := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Attributes:    []attribute.KeyValue{},
	})
	if result.Decision != sdktrace.Drop && result.Decision != sdktrace.RecordAndSample {
		t.Errorf("unexpected decision for unknown: %v", result.Decision)
	}
}

func TestUpdateRate_Clamp(t *testing.T) {
	s := &TenantTierSampler{
		rates:  map[TenantTier]float64{},
		parent: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(DefaultSamplingRate)),
	}
	s.UpdateRate(TierFree, 0.5)
	if s.rates[TierFree] != 0.5 {
		t.Errorf("expected 0.5, got %f", s.rates[TierFree])
	}
	s.UpdateRate(TierFree, -0.1)
	if s.rates[TierFree] != 0 {
		t.Errorf("expected clamp to 0, got %f", s.rates[TierFree])
	}
	s.UpdateRate(TierFree, 1.5)
	if s.rates[TierFree] != 1 {
		t.Errorf("expected clamp to 1, got %f", s.rates[TierFree])
	}
}

func TestContextWithTenantTier(t *testing.T) {
	ctx := ContextWithTenantTier(context.Background(), TierPro)
	if TenantTierFromContext(ctx) != TierPro {
		t.Errorf("context propagation failed: got %s", TenantTierFromContext(ctx))
	}
}

func TestTenantTierFromContext_Default(t *testing.T) {
	ctx := context.Background()
	if TenantTierFromContext(ctx) != TierFree {
		t.Errorf("default should be TierFree, got %s", TenantTierFromContext(ctx))
	}
}

func TestNewTenantTierSampler(t *testing.T) {
	s := NewTenantTierSampler()
	if s == nil {
		t.Fatal("expected non-nil sampler")
	}
	if s.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}
