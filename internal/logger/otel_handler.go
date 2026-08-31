package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
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

	// Endpoint is the OTLP HTTP endpoint for log export.
	// Defaults to "localhost:4318".
	Endpoint string

	// Insecure disables TLS for the OTLP exporter.
	Insecure bool
}

// OTelHandler is an slog.Handler that bridges to OpenTelemetry logging.
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
	preKVs []attribute.KeyValue
	// groups collects open WithGroup calls; they are prepended to attribute keys
	// for *new* per-record attributes only.
	groups []string
	// stderr is the fallback writer; overridable in tests.
	stderr *os.File
}

func NewOTelHandler(cfg OTelHandlerConfig) (*OTelHandler, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "stellabill-backend"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "localhost:4318"
	}

	exporter, err := otlploghttp.New(
		context.Background(),
		otlploghttp.WithEndpoint(cfg.Endpoint),
		otlploghttp.WithInsecure(cfg.Insecure),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTLP log exporter: %w", err)
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)
	global.SetLoggerProvider(provider)

	return &OTelHandler{
		logger: provider,
		stderr: os.Stderr,
	}, nil
}

// Handle implements slog.Handler.
func (h *OTelHandler) Handle(ctx context.Context, r slog.Record) error {
	// Build OTel record.
	// Use simple string attributes for now to avoid API complexity
	var attrs []interface{}

	rec.SetTimestamp(r.Time)
	rec.SetObservedTimestamp(time.Now())
	rec.SetSeverity(slogLevelToOTelSeverity(r.Level))
	rec.SetSeverityText(r.Level.String())
	rec.SetBody(attribute.StringValue(r.Message))

	// Attach pre-registered attributes (keys already qualified at WithAttrs time).
	if len(h.preKVs) > 0 {
		rec.AddAttributes(h.preKVs...)
	}

	// Attach per-record attributes.
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a.Key, a.Value.String())
		return true
	})

	// Inject trace correlation from the active span if available.
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		sc := span.SpanContext()
		rec.AddAttributes(
			attribute.String("trace_id", sc.TraceID().String()),
			attribute.String("span_id", sc.SpanID().String()),
			attribute.String("trace_flags", sc.TraceFlags().String()),
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

func (h *OTelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Convert and bake group-qualified keys now.
	extra := attrsToKVs(attrs, h.groups)
	newKVs := make([]attribute.KeyValue, len(h.preKVs)+len(extra))
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

func (h *OTelHandler) WithGroup(name string) slog.Handler {
	return h
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
func attrsToKVs(attrs []slog.Attr, groupPath []string) []attribute.KeyValue {
	// Use a temporary record as a collector.
	var rec otellog.Record
	h := &OTelHandler{} // lightweight instance, no logger needed
	for _, a := range attrs {
		h.addAttr(&rec, a, groupPath)
	}
	var out []attribute.KeyValue
	rec.WalkAttributes(func(kv attribute.KeyValue) bool {
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
func slogValueToOTelKV(key string, v slog.Value) attribute.KeyValue {
	switch v.Kind() {
	case slog.KindBool:
		return attribute.Bool(key, v.Bool())
	case slog.KindDuration:
		return attribute.Int64(key, int64(v.Duration()))
	case slog.KindFloat64:
		return attribute.Float64(key, v.Float64())
	case slog.KindInt64:
		return attribute.Int64(key, v.Int64())
	case slog.KindString:
		return attribute.String(key, v.String())
	case slog.KindTime:
		return attribute.String(key, v.Time().UTC().Format(time.RFC3339Nano))
	case slog.KindUint64:
		// OTel log API has no uint64 — use int64 with potential truncation only
		// for values that fit, otherwise stringify.
		u := v.Uint64()
		if u <= 1<<63-1 {
			return attribute.Int64(key, int64(u)) //nolint:gosec // bounds checked above
		}
		return attribute.String(key, fmt.Sprintf("%d", u))
	case slog.KindLogValuer:
		// Already resolved by the caller; stringify as fallback.
		return attribute.String(key, v.String())
	default:
		return attribute.String(key, fmt.Sprintf("%v", v.Any()))
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
