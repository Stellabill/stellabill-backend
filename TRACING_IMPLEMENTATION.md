# Tracing Implementation

This document describes the distributed tracing system for the Stellabill backend, including correlation ID propagation, span coverage, sampling strategy, and security guidelines.

---

## Architecture Overview

```
HTTP Request
     │
     ▼
[otelgin middleware]           → root span, W3C propagation headers extracted
     │
     ▼
[RequestLogger middleware]     → generates request_id UUID
     │                           links request_id to active OTel span
     │                           stores request_id in context via correlation pkg
     ▼
[Auth middleware]              → validates JWT, sets callerID/tenantID in context
     │
     ▼
[Handler]                      → child span "handler.<operation>"
     │                           request_id visible as span attribute
     ▼
[Service layer]                → child span if complex business logic
     │
     ▼
[Repository (postgres)]        → child span per DB query
                                  attributes: subscription.id, plan.id, request_id
                                  error recording via span.RecordError()

Background Worker (no HTTP origin):
[Worker.executeJob()]          → root span "worker.executeJob"
                                  attributes: job.id, job.type, subscription.id
                                  linked to parent HTTP trace via TraceLink if ParentTraceID set
     │
     ▼
[BillingExecutor.Execute()]    → child span per job type
                                  "executor.charge" | "executor.invoice" | "executor.reminder"
```

---

## Correlation IDs

Two correlation IDs flow through the system:

| ID | Source | Context key | Span attribute |
|----|--------|-------------|----------------|
| `request_id` | `RequestLogger` middleware (UUID v4) | `correlation.requestIDKey` | `request_id` |
| `job_id` | Caller sets on `Job.ID` (UUID v4) | `correlation.jobIDKey` | `job.id` |

Both IDs are **opaque UUID v4 strings** — they contain no PII, no timestamps, no sequential counters, and no user-identifiable structure. They are safe to log, store, and include in traces.

### Propagation path

```
RequestLogger → c.Set("request_id", id)
             → correlation.WithRequestID(c.Request.Context(), id)  [standard context]
             → span.SetAttributes(attribute.String("request_id", id))  [OTel span]

Worker       → correlation.WithJobID(ctx, job.ID)
             → span.SetAttributes(attribute.String("job.id", job.ID))
```

### Accessing correlation IDs

In any layer that receives a `context.Context`:

```go
import "stellarbill-backend/internal/correlation"

reqID := correlation.RequestIDFromContext(ctx)  // "" if not set
jobID := correlation.JobIDFromContext(ctx)      // "" if not set
```

In Gin handlers:

```go
reqID, _ := c.Get("request_id")  // set by RequestLogger middleware
```

---

## OpenTelemetry Setup

**Package:** `internal/tracing`

**Entry point:** `tracing.InitTracer(serviceName string) (shutdown func, err)`

**Sampler:** `sdktrace.AlwaysSample` (overridden per environment — see §Sampling)

**Propagators:** W3C Trace Context + W3C Baggage (set as global propagator)

```go
otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
    propagation.TraceContext{},
    propagation.Baggage{},
))
```

**Exporter selection** via `TRACING_EXPORTER` env var:

| Value | Behaviour |
|-------|-----------|
| `stdout` (default) | Pretty-prints spans to stdout — development only |
| `otlp` | Sends to OTLP HTTP endpoint (`OTEL_EXPORTER_OTLP_ENDPOINT`) |
| `none` | No-op — disables tracing (sensitive environments) |

---

## Span Coverage

| Layer | Span name | Key attributes |
|-------|-----------|----------------|
| HTTP middleware | set by `otelgin` | `http.method`, `http.route`, `http.status_code` |
| Repository — plans | `PlanRepo.FindByID` | `plan.id`, `request_id`, `job_id` (if present) |
| Repository — subscriptions | `SubscriptionRepo.FindByID` | `subscription.id`, `request_id`, `job_id` |
| Worker — job execution | `worker.executeJob` | `job.id`, `job.type`, `subscription.id`, `job.attempt` |
| Executor — charge | `executor.charge` | `job.id`, `job.type`, `subscription.id` |
| Executor — invoice | `executor.invoice` | `job.id`, `job.type`, `subscription.id` |
| Executor — reminder | `executor.reminder` | `job.id`, `job.type`, `subscription.id` |

All spans set `codes.Error` + `span.RecordError(err)` on failure and `codes.Ok` on success.

---

## Background Job Tracing (Edge Case)

Background jobs may or may not have an originating HTTP request.

**Case 1 — Job created from an HTTP handler:**

```go
// In the handler, store the trace ID on the job before enqueuing:
span := trace.SpanFromContext(c.Request.Context())
job := &worker.Job{
    ID:             uuid.New().String(),
    ParentTraceID:  span.SpanContext().TraceID().String(), // 32-char hex
    Type:           "charge",
    SubscriptionID: subID,
}
```

When the worker executes this job, it creates a `trace.Link` connecting the worker's root span to the HTTP trace. Both traces appear in your backend (Jaeger/Tempo) and are navigable between each other.

**Case 2 — Job created by the scheduler (no HTTP origin):**

```go
job := &worker.Job{
    ID:            uuid.New().String(),
    ParentTraceID: "",  // empty — no HTTP origin
    Type:          "reminder",
    SubscriptionID: subID,
}
```

The worker creates a standalone root span with `job.id` as the entry point. The job is fully traceable even without an HTTP parent.

---

## Sampling Strategy

Sampling is controlled by the `TRACING_SAMPLER` environment variable.

| Environment | Sampler | Effective rate |
|-------------|---------|---------------|
| `development` | `AlwaysSample` | 100% |
| `staging` | `TraceIDRatioBased(0.20)` | 20% |
| `production` | `ParentBased(TraceIDRatioBased(0.05))` | 5% of new traces; inherited by child spans |

**Always-trace overrides (regardless of sampler):**

- Any span with `span.SetStatus(codes.Error, ...)` — errors are always sampled
- Any DB query span with `duration > 1s` — slow queries are always sampled
- Any worker job marked `dead_letter` — dead-lettered jobs are always sampled

**Implementation:** To activate environment-specific sampling, update `tracing.InitTracer()`:

```go
sampler := sdktrace.AlwaysSample()
switch os.Getenv("APP_ENV") {
case "production":
    sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.05))
case "staging":
    sampler = sdktrace.TraceIDRatioBased(0.20)
}
```

**Performance impact:** At 5% sampling in production, tracing overhead is negligible (< 1ms per request on average). The `otlp` exporter uses batch processing to minimise network calls.

---

## W3C Trace Context Propagation

For downstream HTTP calls (payment gateways, notification services), inject the trace context into outgoing requests:

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/propagation"
)

req, _ := http.NewRequestWithContext(ctx, "POST", url, body)
// Inject W3C traceparent + tracestate headers:
otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
```

This adds a `traceparent` header (e.g. `00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01`) that downstream services can use to continue the trace.

---

## Security Guidelines

| Rule | Reason |
|------|--------|
| Correlation IDs must be UUID v4 — never derive from user input | Prevents IDs from encoding PII or being manipulated |
| Never add PII (emails, names, phone numbers) as span attributes | Traces are exported to external backends and may be retained long-term |
| `JWT_SECRET`, `DATABASE_URL`, `ADMIN_TOKEN` must never appear in spans | Treat as a security incident if found; rotate immediately |
| `Authorization` header must not be added to span attributes | The `otelgin` middleware and `RequestLogger` already redact this |
| Tracing can be disabled via `TRACING_EXPORTER=none` | Use in environments where trace data must not leave the host |

---

## Complete Trace Example

**Scenario:** HTTP GET /api/subscriptions/:id

```
Trace ID: 4bf92f3577b34da6a3ce929d0e0e4736
│
├─ [otelgin] GET /api/subscriptions/:id          2ms total
│   request_id: "a1b2c3d4-..."
│   http.status_code: 200
│
├─── [handler] GetSubscription                   1ms
│     request_id: "a1b2c3d4-..."
│     caller_id: "user-xyz"
│
└───── [repo] SubscriptionRepo.FindByID          0.5ms
        subscription.id: "sub-abc"
        request_id: "a1b2c3d4-..."
        status: OK
```

**Scenario:** Background charge job (HTTP-originated)

```
HTTP Trace ID: 4bf92f3577b34da6a3ce929d0e0e4736
  └─ (linked via TraceLink)

Worker Trace ID: 9a3c1d2e8f4b5c6a7d8e9f0a1b2c3d4e
│
├─ [worker] worker.executeJob                    105ms total
│   job.id: "job-uuid-here"
│   job.type: "charge"
│   job.attempt: 1
│   link → HTTP trace 4bf92f35...
│
└─── [executor] executor.charge                  103ms
      job.id: "job-uuid-here"
      subscription.id: "sub-abc"
      status: OK
```

**Scenario:** Scheduler-originated reminder (no HTTP parent)

```
Worker Trace ID: 7f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c
│
├─ [worker] worker.executeJob                    102ms total
│   job.id: "sched-job-uuid"
│   job.type: "reminder"
│   job.attempt: 1
│   (no link — standalone trace)
│
└─── [executor] executor.reminder                101ms
      job.id: "sched-job-uuid"
      subscription.id: "sub-def"
      status: OK
```