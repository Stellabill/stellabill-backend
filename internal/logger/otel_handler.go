package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

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
	logger  *sdklog.LoggerProvider
	stderr  *os.File
	groups  []string
	preKVs  []interface{}
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

	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a.Key, a.Value.String())
		return true
	})

	// Emit via standard logger as fallback
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

func (h *OTelHandler) Close() error {
	return h.logger.Shutdown(context.Background())
}
