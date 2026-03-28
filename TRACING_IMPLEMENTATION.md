# #25 Backend: Tracing Integration for Request Lifecycles

## Overview
Implemented distributed tracing using **OpenTelemetry** (OTEL) to track request lifecycles across the backend layers including middleware, service handlers, and database repositories.

## Key Changes
- **OTEL Setup**: Created `internal/tracing` package to initialize the TracerProvider and configure exporters (`stdout`, `otlp`, `none`).
- **Middleware Integration**:
    - Added `otelgin` middleware to the Gin engine in `internal/routes/routes.go`.
    - Updated `TraceIDMiddleware` in `internal/middleware/traceid.go` to extract and propagate OTEL trace IDs into response headers (`X-Trace-ID`).
- **Instrumentation**:
    - **Repository Layer**: Added spans to `PlanRepo` and `SubscriptionRepo` in `internal/repository/postgres` for DB-level observability.
    - **Service Layer**: Added spans to `SubscriptionService.GetDetail` for business logic observability.
- **Logging Integration**: Hooked `logrus` into OpenTelemetry using `otellogrus` to inject trace contexts into logs.
- **Configuration**: Added `TRACING_EXPORTER` and `TRACING_SERVICE_NAME` to the application config.

## Test Results
- **Trace Propagation**: Verified that child spans in the service and repository layers correctly inherit the Trace ID from the incoming HTTP request.
- **Exporter Config**: Verified that `InitTracer` handles various exporter types and handles initialization failures gracefully.

```
=== RUN   TestTraceContextPropagation
--- PASS: TestTraceContextPropagation (0.00s)
=== RUN   TestTracerExporterConfiguration
=== RUN   TestTracerExporterConfiguration/stdout_exporter
--- PASS: TestTracerExporterConfiguration/stdout_exporter (0.00s)
=== RUN   TestTracerExporterConfiguration/none_exporter
--- PASS: TestTracerExporterConfiguration/none_exporter (0.00s)
=== RUN   TestTracerExporterConfiguration/invalid_exporter
--- PASS: TestTracerExporterConfiguration/invalid_exporter (0.00s)
PASS
ok      stellarbill-backend/internal/tracing    0.363s
```

## Security Notes
- **Context Isolation**: Spans are correctly scoped to the request context, ensuring no cross-request data leakage in traces.
- **Span Attributes**: Sensitive data such as JWT secrets or user passwords are NOT included in span attributes. Only high-level identifiers (e.g., `plan.id`, `subscription.id`) are logged.
- **Configurable Exporters**: Tracing can be completely disabled in production if needed by setting `TRACING_EXPORTER=none`.
- **Error Handling**: Graceful fallback if the tracing exporter is unavailable, ensuring the application remains functional even if observability tools fail.
