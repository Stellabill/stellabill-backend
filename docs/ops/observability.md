# Observability: OTel Logs Bridge

This document describes the OpenTelemetry Logs bridge introduced in
`internal/logger/otel_handler.go` and how to configure, operate, and
troubleshoot it.

---

## Overview

Stellabill emits structured log records via `log/slog`.  When
`OTEL_LOGS_ENABLED=true` those records are forwarded to an OTLP/HTTP endpoint
alongside traces and metrics, giving a single collector endpoint that captures
all three telemetry signals.

```
Application slog call
        │
        ▼
  OTelHandler  ──── batch ────▶  OTLP/HTTP (/v1/logs)  ──▶  Collector / Backend
        │
        ▼
   stderr (fallback — message text only)
```

The bridge is **off by default** (`OTEL_LOGS_ENABLED=false`) so existing
deployments are not affected until explicitly opted in.

---

## Quick-start

```bash
# 1. Enable the bridge
export OTEL_LOGS_ENABLED=true

# 2. Point at your collector (e.g. OpenTelemetry Collector, Grafana Agent)
export OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318

# 3. Optional: override service name (defaults to TRACING_SERVICE_NAME)
export TRACING_SERVICE_NAME=stellabill-backend

# 4. Start the server
go run ./cmd/server
```

Logs will appear in your backend (Loki, Tempo, Jaeger, …) under the
service name you set.

---

## Configuration reference

| Variable | Default | Description |
|---|---|---|
| `OTEL_LOGS_ENABLED` | `false` | Set `true` to activate the OTel Logs bridge. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `https://localhost:4318` | Base URL of your OTLP collector. The exporter appends `/v1/logs`. |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | — | Log-specific override (takes precedence over the generic endpoint). |
| `OTEL_EXPORTER_OTLP_INSECURE` | `false` | Set `true` to disable TLS (for local dev collectors). |
| `OTEL_EXPORTER_OTLP_HEADERS` | — | Comma-separated `key=value` auth headers, e.g. `Authorization=Bearer <token>`. |
| `OTEL_EXPORTER_OTLP_TIMEOUT` | `10s` | Per-flush export deadline. |
| `TRACING_SERVICE_NAME` | `stellabill-backend` | Service name attached to every log record. |

All standard `OTEL_EXPORTER_OTLP_*` environment variables accepted by the Go
OTLP SDK are honoured.  See the
[OTel Go SDK docs](https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp)
for the full list.

---

## Trace / log correlation

When a span is active in the request context, the handler automatically attaches
three attributes to every log record:

| Attribute | Example value |
|---|---|
| `trace_id` | `4bf92f3577b34da6a3ce929d0e0e4736` |
| `span_id` | `00f067aa0ba902b7` |
| `trace_flags` | `01` |

Most observability back-ends (Grafana, Honeycomb, Lightstep, Jaeger) use these
fields to pivot from a log entry to the parent trace.

No additional code changes are required in handlers or services — correlation is
injected transparently by the `OTelHandler.Handle` method.

---

## Backpressure and drop policy

The bridge uses the OTel SDK **BatchProcessor**, which queues records in memory
and flushes them asynchronously.

| Parameter | Default | How to tune |
|---|---|---|
| Queue size | 2 048 records | Increase under high log volume; each record ~1 KB ⇒ ~2 MB default RAM. |
| Batch size | 512 records | Larger batches reduce HTTP overhead; smaller batches reduce latency. |
| Export interval | 5 s | Decrease for lower tail-latency in log delivery. |
| Export timeout | 10 s | Increase only if your collector is slow to ack. |

**When the queue is full** the SDK drops the **oldest enqueued** record
(head-drop) rather than blocking the caller.  This means:

- Application code is **never blocked** by a slow or unavailable collector.
- Under sustained overload, older records are lost before newer ones (LIFO
  priority for recency).
- Dropped record counts are tracked by the OTel SDK's internal metrics
  (`otel_sdk_log_records_dropped_total`); alert on this counter if you run a
  Prometheus scrape endpoint.

**Stderr fallback**: every record is also written as a single line to stderr
before being enqueued.  The line contains only the timestamp, level, and message
text — never attribute values — so it cannot leak PII.  This guarantees that
records are preserved locally even if the OTLP pipeline is completely down.

```
2026-07-29T00:00:00Z    INFO    subscription created
```

---

## Security considerations

1. **No PII in attribute values** — callers must strip PII before logging.  The
   handler does not redact values; this is consistent with the existing
   `internal/security/redactor.go` pattern used throughout the codebase.
2. **Stderr fallback carries message text only** — attribute values are never
   written to stderr.
3. **TLS by default** — `OTEL_EXPORTER_OTLP_INSECURE` defaults to `false`.
   Only set it to `true` within a trusted private network.
4. **Auth headers** — pass API keys via `OTEL_EXPORTER_OTLP_HEADERS`; do not
   hard-code them in source.
5. **Feature flag** — the bridge is a boolean opt-in (`OTEL_LOGS_ENABLED`).
   Disabling it requires no code change.

---

## Local development with the OTel Collector

A minimal `docker-compose` snippet for local development:

```yaml
services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    ports:
      - "4318:4318"   # OTLP/HTTP
    volumes:
      - ./otel-collector.yaml:/etc/otel-collector.yaml
    command: ["--config=/etc/otel-collector.yaml"]
```

`otel-collector.yaml` (print everything to stdout):

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318

exporters:
  debug:
    verbosity: detailed

service:
  pipelines:
    logs:
      receivers: [otlp]
      exporters: [debug]
```

Then run:

```bash
OTEL_LOGS_ENABLED=true \
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 \
OTEL_EXPORTER_OTLP_INSECURE=true \
go run ./cmd/server
```

---

## Shutdown and flush

`InitOTelBridge` returns a `shutdown` function.  Call it during graceful
shutdown so buffered records are flushed before the process exits:

```go
shutdown, err := logger.InitOTelBridge(ctx, cfg.OTelLogsEnabled, cfg.TracingServiceName)
if err != nil {
    // Non-fatal: app continues without OTel logs.
    log.Printf("otel logs bridge not available: %v", err)
}
defer func() {
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    _ = shutdown(shutdownCtx)
}()
```

---

## Runbook: OTLP endpoint unreachable

**Symptom**: `otel_sdk_log_records_dropped_total` counter rising; no logs in
backend; stderr shows records normally.

**Steps**:

1. Verify collector is healthy: `curl -v http://<collector>:4318/v1/logs -d '{}'`
2. Check `OTEL_EXPORTER_OTLP_ENDPOINT` is set correctly.
3. Check TLS: if the collector uses plain HTTP set `OTEL_EXPORTER_OTLP_INSECURE=true`.
4. If the collector is persistently down and drop is unacceptable, set
   `OTEL_LOGS_ENABLED=false` to fall back to stderr-only logging until the
   collector is restored.

---

## Related files

| Path | Purpose |
|---|---|
| `internal/logger/otel_handler.go` | `slog.Handler` implementation |
| `internal/logger/init.go` | `InitOTelBridge` wiring helper |
| `internal/logger/otel_handler_test.go` | Core handler tests |
| `internal/logger/otel_handler_extra_test.go` | Concurrency, helpers, init tests |
| `internal/logger/otel_handler_coverage_test.go` | Coverage gap closers |
| `internal/config/config.go` | `OTelLogsEnabled` field + `OTEL_LOGS_ENABLED` parsing |
| `internal/tracing/tracing.go` | Trace propagator setup |
