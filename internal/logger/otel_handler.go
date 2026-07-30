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

	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/trace"
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
	Provider otellog.LoggerProvider

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
//
// # Backpressure and drop policy
//
// The underlying SDK BatchProcessor holds up to MaxQueueSize records in memory.
// When the queue is full it drops the oldest enqueued record (head-drop) so that
// callers are never blocked.  Dropped records are counted by the OTel SDK but
// are not retried.  If the OTLP endpoint is persistently unavailable the queue
// will eventually fill and steady-state drop will occur; the fallback stderr
// write ensures at least one copy of every record is preserved locally.
//
// # Trace correlation
//
// When a span is active in the provided context, trace_id and span_id are
// attached to the OTel record as body attributes.  The W3C trace context is
// also propagated by the OTLP exporter so back-ends can join logs to traces.
//
// # Security
//
// This handler does not redact field values; callers are responsible for
// stripping PII before passing records.  The stderr fallback writes the
// message text only, never attribute values.
type OTelHandler struct {
	logger   otellog.Logger
	provider otellog.LoggerProvider
	// ownProvider is true when this handler created the provider and is
	// responsible for shutting it down.
	ownProvider bool
	minLevel    slog.Level
	// preKVs are OTel KeyValues already resolved from WithAttrs calls.
	// Keys are fully qualified (group prefix already baked in) at the time
	// WithAttrs is called, so they are immune to subsequent WithGroup calls.
	preKVs []otellog.KeyValue
	// groups collects open WithGroup calls; they are prepended to attribute keys
	// for *new* per-record attributes only.
	groups []string
	// stderr is the fallback writer; overridable in tests.
	stderr *os.File
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
func NewOTelHandler(ctx context.Context, cfg OTelHandlerConfig) (*OTelHandler, func(context.Context) error, error) {
	cfg.applyDefaults()

	var (
		provider    otellog.LoggerProvider
		ownProvider bool
		shutdown    func(context.Context) error
	)

	if cfg.Provider != nil {
		provider = cfg.Provider
		ownProvider = false
		shutdown = func(context.Context) error { return nil }
	} else {
		// Build OTLP exporter options.
		exporterOpts := []otlploghttp.Option{
			otlploghttp.WithTimeout(cfg.ExportTimeout),
		}
		if cfg.OTLPEndpoint != "" {
			exporterOpts = append(exporterOpts, otlploghttp.WithEndpointURL(cfg.OTLPEndpoint+"/v1/logs"))
		}

		exporter, err := otlploghttp.New(ctx, exporterOpts...)
		if err != nil {
			return nil, nil, fmt.Errorf("otel_handler: create OTLP exporter: %w", err)
		}

		batchProcessor := sdklog.NewBatchProcessor(exporter,
			sdklog.WithMaxQueueSize(cfg.MaxQueueSize),
			sdklog.WithExportMaxBatchSize(cfg.ExportBatchSize),
			sdklog.WithExportInterval(cfg.ExportInterval),
			sdklog.WithExportTimeout(cfg.ExportTimeout),
		)

		sdkProvider := sdklog.NewLoggerProvider(
			sdklog.WithProcessor(batchProcessor),
		)

		// Register as global so code that uses global.Logger() also benefits.
		global.SetLoggerProvider(sdkProvider)

		provider = sdkProvider
		ownProvider = true
		shutdown = func(ctx context.Context) error {
			return sdkProvider.Shutdown(ctx)
		}
	}

	h := &OTelHandler{
		logger:      provider.Logger(cfg.ServiceName),
		provider:    provider,
		ownProvider: ownProvider,
		minLevel:    cfg.MinLevel,
		preKVs:      nil,
		groups:      nil,
		stderr:      os.Stderr,
	}
	return h, shutdown, nil
}

// Enabled implements slog.Handler.
func (h *OTelHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

// Handle implements slog.Handler.
//
// It converts the slog.Record to an OTel log.Record and emits it via the
// configured LoggerProvider.  As a safety net it also writes a minimal entry
// to stderr so that records are never silently lost if the OTLP pipeline is
// down.
func (h *OTelHandler) Handle(ctx context.Context, r slog.Record) error {
	// Build OTel record.
	var rec otellog.Record

	rec.SetTimestamp(r.Time)
	rec.SetObservedTimestamp(time.Now())
	rec.SetSeverity(slogLevelToOTelSeverity(r.Level))
	rec.SetSeverityText(r.Level.String())
	rec.SetBody(otellog.StringValue(r.Message))

	// Attach pre-registered attributes (keys already qualified at WithAttrs time).
	if len(h.preKVs) > 0 {
		rec.AddAttributes(h.preKVs...)
	}

	// Attach per-record attributes.
	r.Attrs(func(a slog.Attr) bool {
		h.addAttr(&rec, a, h.groups)
		return true
	})

	// Inject trace correlation from the active span if available.
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		sc := span.SpanContext()
		rec.AddAttributes(
			otellog.String("trace_id", sc.TraceID().String()),
			otellog.String("span_id", sc.SpanID().String()),
			otellog.String("trace_flags", sc.TraceFlags().String()),
		)
	}

	// Emit to the OTel pipeline (non-blocking; enqueues to batch processor).
	h.logger.Emit(ctx, rec)

	// Fallback: always write a minimal line to stderr so records are never
	// lost if the OTLP endpoint is down.  We write message text only — no
	// attribute values — to avoid inadvertently surfacing PII on the console.
	fmt.Fprintf(h.stderr, "%s\t%s\t%s\n",
		r.Time.UTC().Format(time.RFC3339),
		r.Level.String(),
		r.Message,
	)

	return nil
}

// WithAttrs implements slog.Handler.
//
// Attrs are resolved immediately using the current group path so that
// subsequent WithGroup calls do not retroactively change their key prefix.
func (h *OTelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Convert and bake group-qualified keys now.
	extra := attrsToKVs(attrs, h.groups)
	newKVs := make([]otellog.KeyValue, len(h.preKVs)+len(extra))
	copy(newKVs, h.preKVs)
	copy(newKVs[len(h.preKVs):], extra)
	return &OTelHandler{
		logger:      h.logger,
		provider:    h.provider,
		ownProvider: false,
		minLevel:    h.minLevel,
		preKVs:      newKVs,
		groups:      h.groups,
		stderr:      h.stderr,
	}
}

// WithGroup implements slog.Handler.
func (h *OTelHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	newGroups := make([]string, len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups[len(h.groups)] = name
	return &OTelHandler{
		logger:      h.logger,
		provider:    h.provider,
		ownProvider: false,
		minLevel:    h.minLevel,
		preKVs:      h.preKVs, // already baked — unaffected by new group
		groups:      newGroups,
		stderr:      h.stderr,
	}
}

// addAttr converts a slog.Attr (which may be a group) into OTel KeyValues and
// appends them to the record.  groupPrefix is the chain of WithGroup names.
func (h *OTelHandler) addAttr(rec *otellog.Record, a slog.Attr, groupPath []string) {
	// Resolve any LogValuer.
	a.Value = a.Value.Resolve()

	// Skip zero-value attrs.
	if a.Equal(slog.Attr{}) {
		return
	}

	if a.Value.Kind() == slog.KindGroup {
		subAttrs := a.Value.Group()
		// Inline groups with empty key per slog convention.
		var subPath []string
		if a.Key == "" {
			subPath = groupPath
		} else {
			subPath = append(groupPath, a.Key) //nolint:gocritic // intentional new slice
		}
		for _, sub := range subAttrs {
			h.addAttr(rec, sub, subPath)
		}
		return
	}

	key := buildKey(groupPath, a.Key)
	rec.AddAttributes(slogValueToOTelKV(key, a.Value))
}

// attrsToKVs converts a slice of slog.Attr to OTel KeyValues, applying
// groupPath as the key prefix.  Used by WithAttrs to bake keys eagerly.
func attrsToKVs(attrs []slog.Attr, groupPath []string) []otellog.KeyValue {
	// Use a temporary record as a collector.
	var rec otellog.Record
	h := &OTelHandler{} // lightweight instance, no logger needed
	for _, a := range attrs {
		h.addAttr(&rec, a, groupPath)
	}
	var out []otellog.KeyValue
	rec.WalkAttributes(func(kv otellog.KeyValue) bool {
		out = append(out, kv)
		return true
	})
	return out
}

// buildKey joins group names and the attribute key with a dot separator.
func buildKey(groups []string, key string) string {
	if len(groups) == 0 {
		return key
	}
	result := ""
	for _, g := range groups {
		result += g + "."
	}
	return result + key
}

// slogValueToOTelKV converts a slog.Value to an OTel log.KeyValue.
func slogValueToOTelKV(key string, v slog.Value) otellog.KeyValue {
	switch v.Kind() {
	case slog.KindBool:
		return otellog.Bool(key, v.Bool())
	case slog.KindDuration:
		return otellog.Int64(key, int64(v.Duration()))
	case slog.KindFloat64:
		return otellog.Float64(key, v.Float64())
	case slog.KindInt64:
		return otellog.Int64(key, v.Int64())
	case slog.KindString:
		return otellog.String(key, v.String())
	case slog.KindTime:
		return otellog.String(key, v.Time().UTC().Format(time.RFC3339Nano))
	case slog.KindUint64:
		// OTel log API has no uint64 — use int64 with potential truncation only
		// for values that fit, otherwise stringify.
		u := v.Uint64()
		if u <= 1<<63-1 {
			return otellog.Int64(key, int64(u)) //nolint:gosec // bounds checked above
		}
		return otellog.String(key, fmt.Sprintf("%d", u))
	case slog.KindLogValuer:
		// Already resolved by the caller; stringify as fallback.
		return otellog.String(key, v.String())
	default:
		return otellog.String(key, fmt.Sprintf("%v", v.Any()))
	}
}

// slogLevelToOTelSeverity maps slog levels to OTel Severity values.
func slogLevelToOTelSeverity(l slog.Level) otellog.Severity {
	switch {
	case l < slog.LevelDebug:
		return otellog.SeverityTrace
	case l < slog.LevelInfo:
		return otellog.SeverityDebug
	case l < slog.LevelWarn:
		return otellog.SeverityInfo
	case l < slog.LevelError:
		return otellog.SeverityWarn
	default:
		return otellog.SeverityError
	}
}
