# Request-Level Tracing Sampler (Tenant-Tier Based)

## Overview
Introduce a sampling strategy that adjusts trace collection rates based on tenant subscription tier.

## Architecture

### Sampler Interface
```go
type TenantSampler interface {
    ShouldSample(ctx context.Context, tenantID string) bool
    GetSamplingRate(tier TenantTier) float64
}

type TenantTier string
const (
    TierFree       TenantTier = "free"
    TierPro        TenantTier = "pro"
    TierEnterprise TenantTier = "enterprise"
)
```

### Sampling Rates
| Tier | Rate | Rationale |
|------|------|-----------|
| Free | 10% | Cost control, still catch critical errors |
| Pro | 50% | Good observability without excessive cost |
| Enterprise | 100% | Full trace coverage for compliance/SLA |

### Implementation
```go
type tenantTierSampler struct {
    tierCache   *TierCache
    rateLimiter *RateLimiter
}

func (s *tenantTierSampler) ShouldSample(ctx context.Context, tenantID string) bool {
    tier := s.tierCache.Get(tenantID)
    rate := s.GetSamplingRate(tier)
    return rand.Float64() < rate
}
```

### Integration Points
- gRPC interceptors
- HTTP middleware
- Message queue consumers (NATS/Kafka)

### Testing
- Unit: verify sampling rates match tier config
- Integration: end-to-end trace with tier override
- Benchmark: sampler overhead < 1μs per decision
