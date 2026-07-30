package tracing

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type TenantTier string

const (
	TierFree       TenantTier = "free"
	TierPro        TenantTier = "pro"
	TierEnterprise TenantTier = "enterprise"
)

var TierSamplingRates = map[TenantTier]float64{
	TierFree:       0.01,
	TierPro:        0.25,
	TierEnterprise: 1.00,
}

const DefaultSamplingRate = 0.01
const TierAttributeKey = "tenant.tier"

type TenantTierSampler struct {
	mu     sync.RWMutex
	rates  map[TenantTier]float64
	parent sdktrace.Sampler
}

func NewTenantTierSampler() sdktrace.Sampler {
	return &TenantTierSampler{
		rates:  TierSamplingRates,
		parent: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(DefaultSamplingRate)),
	}
}

func (s *TenantTierSampler) ShouldSample(params sdktrace.SamplingParameters) sdktrace.SamplingResult {
	if params.ParentContext.HasTraceID() {
		return s.parent.ShouldSample(params)
	}
	rate := DefaultSamplingRate
	for _, attr := range params.Attributes {
		if attr.Key == TierAttributeKey {
			switch attr.Value.AsString() {
			case "enterprise":
				rate = TierSamplingRates[TierEnterprise]
			case "pro":
				rate = TierSamplingRates[TierPro]
			}
			break
		}
	}
	return sdktrace.TraceIDRatioBased(rate).ShouldSample(params)
}

func (s *TenantTierSampler) Description() string {
	return "TenantTierSampler{free=1%, pro=25%, enterprise=100%}"
}

func (s *TenantTierSampler) UpdateRate(tier TenantTier, rate float64) {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	s.mu.Lock()
	s.rates[tier] = rate
	s.mu.Unlock()
}

type contextKey struct{}

var tenantTierContextKey = contextKey{}

func ContextWithTenantTier(ctx context.Context, tier TenantTier) context.Context {
	return context.WithValue(ctx, tenantTierContextKey, tier)
}

func TenantTierFromContext(ctx context.Context) TenantTier {
	if tier, ok := ctx.Value(tenantTierContextKey).(TenantTier); ok {
		return tier
	}
	return TierFree
}
