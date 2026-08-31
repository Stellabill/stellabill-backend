package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// AllowedBaggageKeys enforces the strict allowlist for baggage attributes to prevent PII leaks.
var AllowedBaggageKeys = map[string]bool{
	"tenant_id":   true,
	"customer_id": true,
}

// InitPropagators registers both W3C TraceContext and Baggage propagators.
func InitPropagators() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

// BaggageSpanProcessor is a custom processor that stamps allowed baggage onto spans.
type BaggageSpanProcessor struct{}

// OnStart reads baggage from the context and adds allowed items as span attributes.
func (bsp BaggageSpanProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	bag := baggage.FromContext(parent)
	for _, member := range bag.Members() {
		if AllowedBaggageKeys[member.Key()] {
			s.SetAttributes(attribute.String(member.Key(), member.Value()))
		}
	}
}

// Shutdown is a no-op for this processor.
func (bsp BaggageSpanProcessor) Shutdown(context.Context) error { return nil }

// ForceFlush is a no-op for this processor.
func (bsp BaggageSpanProcessor) ForceFlush(context.Context) error { return nil }

// OnEnd is a no-op for this processor.
func (bsp BaggageSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {}

// SetupTestTracerProvider initializes an in-memory span exporter and sets it as global tracer provider for testing.
func SetupTestTracerProvider() (*tracetest.InMemoryExporter, func()) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(InitPropagators())

	shutdown := func() {
		_ = tp.Shutdown(context.Background())
	}
	return exporter, shutdown
}
