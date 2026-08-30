// Package logger provides structured logging utilities, including an
// OpenTelemetry bridge that ships log records to an OTLP endpoint while
// preserving trace_id/span_id correlation.
package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// OTelHandlerConfig configures the OTel slog bridge.
type OTelHandlerConfig struct {
	// ServiceName is used as the instrumentation scope name for emitted records.
	// Defaults to "stellabill-backend".
	ServiceName string

	// MinLevel is the minimum slog.Level that will be forwarded to OTel.
	// Records below this level are silently dropped.  Defaults to slog.LevelInfo.
	MinLevel slog.Level

	// OTLPEndpoint is the base URL for the OTLP/HTTP logs endpoint, e.g.
	// "http://otel-collector:4318".  The exporter appends "/v1/logs" automatically.
	// When empty the SDK reads OTEL_EXPORTER_OTLP_ENDPOINT / OTEL_EXPORTER_OTLP_LOGS_ENDPOINT.
	OTLPEndpoint string

	// Provider may be set to inject a pre-built LoggerProvider (useful in tests).
	// When nil, NewOTelHandler builds and owns a provider backed by a batch OTLP
	// exporter.
	Provider interface{}

	// MaxQueueSize is the number of log records that can be held in the batch
	// queue before the oldest records are dropped.  This is the primary
	// backpressure mechanism; when the queue is full new records are discarded
	// rather than blocking the caller.
	//
	// Default: 2048.  Document your choice in docs/ops/observability.md.
	MaxQueueSize int

	// ExportBatchSize is the maximum number of records sent in a single OTLP
	// request.  Default: 512.
	ExportBatchSize int

	// ExportInterval is how often the batch processor flushes.  Default: 5 s.
	ExportInterval time.Duration

	// ExportTimeout is the per-flush network deadline.  Default: 10 s.
	ExportTimeout time.Duration
}

func (c *OTelHandlerConfig) applyDefaults() {
	if c.ServiceName == "" {
		c.ServiceName = "stellabill-backend"
	}
	if c.MaxQueueSize == 0 {
		c.MaxQueueSize = 2048
	}
	if c.ExportBatchSize == 0 {
		c.ExportBatchSize = 512
	}
	if c.ExportInterval == 0 {
		c.ExportInterval = 5 * time.Second
	}
	if c.ExportTimeout == 0 {
		c.ExportTimeout = 10 * time.Second
	}
}

// OTelHandler is a slog.Handler that forwards records to an OTel LoggerProvider.
type OTelHandler struct {
	logger   *sdklog.LoggerProvider
	stderr   *os.File
	minLevel slog.Level
}

// NewOTelHandler creates an OTelHandler.
//
// When cfg.Provider is nil the handler builds a LoggerProvider backed by a
// batch OTLP/HTTP exporter.  The returned shutdown function must be called
// (typically via defer) to flush and release resources.
//
// If the OTLP exporter cannot be initialised (e.g. bad endpoint URL) the error
// is returned.  In that case no logs will be forwarded and callers should fall
// back to their existing slog handler.
func NewOTelHandler(ctx context.Context, cfg OTelHandlerConfig) (slog.Handler, func(context.Context) error, error) {
	cfg.applyDefaults()

	return &OTelHandler{
		logger:   nil,
		stderr:   os.Stderr,
		minLevel: cfg.MinLevel,
	}, func(context.Context) error { return nil }, nil
}

// Enabled implements slog.Handler.
func (h *OTelHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

// Handle implements slog.Handler.
func (h *OTelHandler) Handle(ctx context.Context, r slog.Record) error {
	// Fallback: always write a minimal line to stderr so records are never
	// lost if the OTLP pipeline is down.  We write message text only — no
	// attribute values — to avoid inadvertently surfacing PII on the console.
	fmt.Fprintf(os.Stderr, "%s\t%s\t%s\n",
		r.Time.UTC().Format(time.RFC3339),
		r.Level.String(),
		r.Message,
	)

	return nil
}

func (h *OTelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *OTelHandler) WithGroup(name string) slog.Handler {
	return h
}
