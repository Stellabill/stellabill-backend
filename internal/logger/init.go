package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// InitOTelBridge sets up the OpenTelemetry log bridge when enabled is true.
//
// It returns a shutdown function that must be called (e.g. via defer) to flush
// buffered records and release resources.  When enabled is false a no-op
// shutdown is returned and no OTel machinery is initialised.
//
// The bridge installs a slog.Handler that:
//   - Forwards every slog record ≥ minLevel to an OTLP/HTTP endpoint.
//   - Attaches trace_id and span_id from the active OTel span in the context.
//   - Writes a minimal fallback line to stderr for operational resilience.
//
// Configuration is taken from well-known environment variables:
//
//	OTEL_LOGS_ENABLED              – "true" activates the bridge
//	OTEL_EXPORTER_OTLP_ENDPOINT   – base URL, e.g. http://collector:4318
//	TRACING_SERVICE_NAME           – service name attached to log records
//
// See docs/ops/observability.md for the full configuration reference.
func InitOTelBridge(ctx context.Context, enabled bool, serviceName string) (shutdown func(context.Context) error, err error) {
	noop := func(context.Context) error { return nil }

	if !enabled {
		return noop, nil
	}

	cfg := OTelHandlerConfig{
		ServiceName: serviceName,
		MinLevel:    slog.LevelInfo,
	}

	handler, shutdownFn, err := NewOTelHandler(ctx, cfg)
	if err != nil {
		// Failure to initialise the bridge is non-fatal; fall back to the
		// default slog handler so the application keeps running.
		fmt.Fprintf(os.Stderr, "otel_bridge: failed to initialise OTLP log exporter: %v — continuing without OTel logs\n", err)
		return noop, err
	}

	// Replace the default slog handler with the OTel bridge.
	slog.SetDefault(slog.New(handler))

	return shutdownFn, nil
}
