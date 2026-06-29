package tracing

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// InitTracer initializes an OpenTelemetry tracer provider and exporter.
// It returns a shutdown function that should be called when the application exits.
func InitTracer(serviceName string) (func(context.Context) error, error) {
	ctx := context.Background()

	tailConfig, err := tailConfigFromEnv()
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	var exporter sdktrace.SpanExporter
	exporterType := os.Getenv("TRACING_EXPORTER")
	if exporterType == "" {
		exporterType = "stdout"
	}

	switch exporterType {
	case "otlp":
		// This will use default OTLP environment variables:
		// OTEL_EXPORTER_OTLP_ENDPOINT, etc.
		exporter, err = otlptracehttp.New(ctx)
	case "stdout":
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	case "none":
		// No-op tracer provider is already the default in OTEL
		return func(context.Context) error { return nil }, nil
	default:
		return nil, fmt.Errorf("unrecognized exporter type: %s", exporterType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	// ParentBased(AlwaysSample) preserves the previous behavior for local root
	// spans while respecting an upstream parent's sampling decision.
	parentSampler := sdktrace.ParentBased(sdktrace.AlwaysSample())
	sampler := sdktrace.Sampler(parentSampler)
	processor := sdktrace.SpanProcessor(sdktrace.NewBatchSpanProcessor(exporter))
	if tailConfig.enabled {
		// A Sampler only sees a span at start time. TailSampler records spans so
		// the bounded processor can decide using their completed state.
		parentSampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(tailConfig.baselineRate))
		sampler = newTailSampler(parentSampler)
		processor = newTailSpanProcessor(processor, tailConfig)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(processor),
	)
	otel.SetTracerProvider(tracerProvider)

	// Set global propagator to tracecontext and baggage.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return tracerProvider.Shutdown, nil
}

const (
	defaultTailLatency    = time.Second
	defaultTailErrorRate  = 0.05
	defaultDecisionWindow = 2 * time.Second
	defaultMaxTraces      = 1_024
	defaultMaxSpans       = 64
)

type tailConfig struct {
	enabled        bool
	latency        time.Duration
	baselineRate   float64
	decisionWindow time.Duration
	maxTraces      int
	maxSpans       int
}

func tailConfigFromEnv() (tailConfig, error) {
	cfg := tailConfig{
		latency:        defaultTailLatency,
		baselineRate:   defaultTailErrorRate,
		decisionWindow: defaultDecisionWindow,
		maxTraces:      defaultMaxTraces,
		maxSpans:       defaultMaxSpans,
	}

	if value := os.Getenv("TRACING_TAIL_ENABLED"); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return cfg, fmt.Errorf("TRACING_TAIL_ENABLED must be a boolean: %w", err)
		}
		cfg.enabled = enabled
	}
	if !cfg.enabled {
		return cfg, nil
	}
	if value := os.Getenv("TRACING_TAIL_LATENCY_MS"); value != "" {
		ms, err := strconv.ParseInt(value, 10, 64)
		if err != nil || ms < 1 || ms > int64((10*time.Minute)/time.Millisecond) {
			return cfg, fmt.Errorf("TRACING_TAIL_LATENCY_MS must be between 1 and 600000")
		}
		cfg.latency = time.Duration(ms) * time.Millisecond
	}
	if value := os.Getenv("TRACING_TAIL_ERROR_RATE"); value != "" {
		rate, err := strconv.ParseFloat(value, 64)
		if err != nil || rate < 0 || rate > 1 {
			return cfg, fmt.Errorf("TRACING_TAIL_ERROR_RATE must be between 0 and 1")
		}
		cfg.baselineRate = rate
	}

	return cfg, nil
}
