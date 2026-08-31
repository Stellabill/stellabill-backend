package tracing

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	DefaultEnterpriseRatio = 1.0
	DefaultFreeRatio       = 0.01
	DefaultGlobalRatio     = 0.05
)

var tracesSampledTotal *prometheus.CounterVec

func initMetrics() {
	tracesSampledTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tracing_sampled_total",
			Help: "Total number of trace sampling decisions, by tenant tier",
		},
		[]string{"tier"},
	)
	prometheus.Register(tracesSampledTotal)
}

func init() {
	initMetrics()
}

// TenantAwareSampler is a head sampler that delegates to
// ParentBased(TraceIDRatioBased) samplers keyed by the tenant tier attribute.
// Enterprise tier uses enterpriseRatio, free tier uses freeRatio, and any
// other or missing tier uses defaultRatio.
type TenantAwareSampler struct {
	enterprise sdktrace.Sampler
	free       sdktrace.Sampler
	defaultS   sdktrace.Sampler
}

// NewTenantAwareSampler creates a TenantAwareSampler with the given ratios
// for each tier. Negative ratios are clamped to zero; ratios above 1 are
// clamped to 1.
func NewTenantAwareSampler(enterpriseRatio, freeRatio, defaultRatio float64) *TenantAwareSampler {
	clamp := func(r float64) float64 {
		if r < 0 {
			return 0
		}
		if r > 1 {
			return 1
		}
		return r
	}
	return &TenantAwareSampler{
		enterprise: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(clamp(enterpriseRatio))),
		free:       sdktrace.ParentBased(sdktrace.TraceIDRatioBased(clamp(freeRatio))),
		defaultS:   sdktrace.ParentBased(sdktrace.TraceIDRatioBased(clamp(defaultRatio))),
	}
}

// ShouldSample inspects the tier attribute (or baggage) and delegates to the
// appropriate tier sampler. It then increments the tracing_sampled_total
// counter with the detected tier.
func (s *TenantAwareSampler) ShouldSample(params sdktrace.SamplingParameters) sdktrace.SamplingResult {
	tier := extractTier(params)
	sampler := s.samplerForTier(tier)
	result := sampler.ShouldSample(params)
	tracesSampledTotal.WithLabelValues(tier).Inc()
	return result
}

// Description returns a human-readable description of the sampler.
func (s *TenantAwareSampler) Description() string {
	return fmt.Sprintf("TenantAware{enterprise=%s, free=%s, default=%s}",
		s.enterprise.Description(), s.free.Description(), s.defaultS.Description())
}

func (s *TenantAwareSampler) samplerForTier(tier string) sdktrace.Sampler {
	switch tier {
	case "enterprise":
		return s.enterprise
	case "free":
		return s.free
	default:
		return s.defaultS
	}
}

// extractTier reads the tenant tier from span attributes first and falls back
// to baggage if not found among attributes.
func extractTier(params sdktrace.SamplingParameters) string {
	for _, a := range params.Attributes {
		if a.Key == "tier" {
			return a.Value.AsString()
		}
	}
	bag := baggage.FromContext(params.ParentContext)
	if member := bag.Member("tier"); member.Key() != "" {
		return member.Value()
	}
	return "unknown"
}

// InitTracer creates a TracerProvider with a TenantAwareSampler and sets it
// as the global tracer provider along with W3C TraceContext + Baggage
// propagators. Ratios are read from environment variables:
//
//	TRACING_ENTERPRISE_RATIO  (default 1.0  = 100%)
//	TRACING_FREE_RATIO        (default 0.01 =   1%)
//	TRACING_DEFAULT_RATIO     (default 0.05 =   5%)
//	TRACING_TAIL_ENABLED      (default false; enables tail-based sampling)
//	TRACING_TAIL_LATENCY_MS   (default 500; ms threshold for slow-request keep)
//	TRACING_TAIL_ERROR_RATE   (default 0;   baseline fraction for dropped traces)
//
// When TRACING_TAIL_ENABLED=true the provider is wrapped with a tail sampler
// that promotes spans with 5xx status codes, latency above the threshold, or
// an error attribute/event to RecordAndSample regardless of the head decision.
// The head (TenantAwareSampler) decision is always preserved for the baseline.
//
// The returned shutdown function drains and shuts down the provider.
func InitTracer(serviceName string) (func() error, error) {
	enterpriseRatio := getEnvFloat("TRACING_ENTERPRISE_RATIO", DefaultEnterpriseRatio)
	freeRatio := getEnvFloat("TRACING_FREE_RATIO", DefaultFreeRatio)
	defaultRatio := getEnvFloat("TRACING_DEFAULT_RATIO", DefaultGlobalRatio)

	headSampler := NewTenantAwareSampler(enterpriseRatio, freeRatio, defaultRatio)

	tailCfg, err := tailConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("tracing: invalid tail-sampling config: %w", err)
	}

	var opts []sdktrace.TracerProviderOption
	opts = append(opts, sdktrace.WithSpanProcessor(BaggageSpanProcessor{}))

	if tailCfg.enabled {
		// Wrap the head sampler so every span is recorded temporarily while
		// we wait for the root span to arrive with the full latency/error signal.
		opts = append(opts, sdktrace.WithSampler(newTailSampler(headSampler)))
		batchProcessor := sdktrace.NewSimpleSpanProcessor(tracetest.NewNoopExporter())
		tailProcessor := newTailSpanProcessor(batchProcessor, tailCfg)
		opts = append(opts, sdktrace.WithSpanProcessor(tailProcessor))
	} else {
		opts = append(opts, sdktrace.WithSampler(headSampler))
	}

	provider := sdktrace.NewTracerProvider(opts...)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(InitPropagators())

	return func() error {
		return provider.Shutdown(context.Background())
	}, nil
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			return f
		}
	}
	return fallback
}
