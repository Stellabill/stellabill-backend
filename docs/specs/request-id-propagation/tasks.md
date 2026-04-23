# Implementation Plan: Request ID Propagation

## Overview

Implement a canonical `internal/requestid` package, consolidate the two competing HTTP middleware implementations into one, extend propagation to audit entries and the worker layer, and add trusted-source allowlisting backed by config.

## Tasks

- [x] 1. Create `internal/requestid` package with core primitives
  - Create `internal/requestid/requestid.go` with constants `ContextKey`, `HeaderName`, `JobIDKey`, `WorkerIDKey`
  - Implement `Generate() string` — 24-hex cryptographically random ID with `time.Now()` fallback
  - Implement `Sanitize(value string) (string, bool)` — accept `[a-zA-Z0-9\-_.]`, max 128 chars, reject empty/whitespace
  - Implement `IsTrustedSource(remoteAddr string, allowlist []net.IPNet) bool` — CIDR matching
  - Implement `WithRequestID`, `FromContext`, `WithJobID`, `JobIDFromContext`, `WithWorkerID`, `WorkerIDFromContext` context helpers
  - _Requirements: 1.2, 2.1, 2.2, 2.3, 2.5, 3.3, 4.2, 7.2, 7.5_

  - [ ]* 1.1 Write property tests for `Sanitize` (Properties 1 & 2)
    - Use `pgregory.net/rapid` to generate random strings; classify valid/invalid; assert `Sanitize` agrees
    - Cover boundary lengths (1, 128, 129), empty string, whitespace-only, special chars as edge cases
    - **Property 1: Sanitizer accepts valid IDs unchanged**
    - **Property 2: Sanitizer rejects invalid IDs**
    - **Validates: Requirements 2.1, 2.2, 2.3, 2.5**

  - [ ]* 1.2 Write property tests for `Generate` (Property 3)
    - Assert every generated ID passes `Sanitize`, has length 24, and is hex-only
    - **Property 3: Generated IDs are always valid**
    - **Validates: Requirements 1.2**

  - [ ]* 1.3 Write property tests for `IsTrustedSource` (Property 10)
    - Generate random IPs and CIDRs; assert result matches `net.IPNet.Contains`
    - **Property 10: IsTrustedSource CIDR matching is consistent**
    - **Validates: Requirements 3.3**

- [x] 2. Add `RequestIDTrustedProxies` to config
  - Add `RequestIDTrustedProxies []net.IPNet` field to `internal/config/config.go`
  - Parse `REQUEST_ID_TRUSTED_PROXIES` env var (comma-separated CIDRs) in `Config.validate()`
  - Log a warning and skip invalid CIDR entries rather than failing startup
  - Add `REQUEST_ID_TRUSTED_PROXIES` to `optionalEnvVars` map with empty default
  - _Requirements: 9.1, 9.2, 9.3, 9.4_

  - [ ]* 2.1 Write unit tests for config CIDR parsing
    - Test valid single CIDR, multiple CIDRs, empty string, invalid CIDR (warning + skip), mixed valid/invalid
    - _Requirements: 9.1, 9.2, 9.3_

- [x] 3. Replace `RequestID()` with `RequestIDWithConfig()` in `internal/middleware/middleware.go`
  - Add `RequestIDConfig` struct with `TrustedProxies []net.IPNet` field
  - Implement `RequestIDWithConfig(cfg RequestIDConfig) gin.HandlerFunc` using `requestid` package
    - Extract `X-Request-ID` header; run `requestid.Sanitize`; check `requestid.IsTrustedSource`
    - Accept inbound ID if trusted and valid; otherwise call `requestid.Generate()`
    - Set Gin context key via `c.Set(requestid.ContextKey, id)` and `requestid.WithRequestID(c.Request.Context(), id)`
    - Write `X-Request-ID` response header
    - Log at DEBUG when discarding a spoofed inbound ID
  - Keep `RequestID()` as a zero-config shim (`RequestIDWithConfig(RequestIDConfig{})`) for backward compat
  - Remove the duplicate `RequestIDKey` and `RequestIDHeader` constants; import from `requestid` package
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 2.4, 3.1, 3.2, 3.4, 3.5, 3.6_

  - [ ]* 3.1 Write property tests for middleware trusted/untrusted routing (Properties 4, 5, 9)
    - Use `net/http/httptest`; generate random valid IDs and IPs; assert context value matches expected routing
    - **Property 4: Trusted-source acceptance round-trip**
    - **Property 5: Untrusted-source ID is always replaced**
    - **Property 9: Empty allowlist always generates new ID**
    - **Validates: Requirements 1.3, 1.4, 3.2, 3.4, 3.5**

  - [ ]* 3.2 Write property test for response header echo (Property 6)
    - For any request, assert `X-Request-ID` response header == Gin context `request_id`
    - **Property 6: Response header echoes context ID**
    - **Validates: Requirements 1.5**

- [x] 4. Remove `middleware.RequestLogger()` and update `Logging()` middleware
  - Delete `RequestLogger()` from `internal/middleware/logger.go` (or remove the file if it only contains that function)
  - Update `Logging()` in `middleware.go` to read `request_id` from context via `requestid.FromContext` rather than generating a new UUID
  - Update `Recovery()` and `RecoveryLogger()` to read `request_id` from context via `requestid.ContextKey`
  - Ensure all error response bodies (`AbortWithStatusJSON`) include `"request_id"` from context
  - _Requirements: 4.1, 4.5, 6.1, 6.2, 6.3, 6.4, 8.1, 8.2_

  - [ ]* 4.1 Write unit tests for error envelope shape
    - Trigger 401, 429, 500 (panic) responses; assert JSON body contains `"request_id"` field matching context
    - _Requirements: 6.1, 6.2, 6.3, 6.4_

- [x] 5. Propagate `request_id` into audit entries
  - Update `audit.LogAction` in `internal/audit/middleware.go` to read `request_id` from Gin context and add it to the metadata map before calling `logger.Log`
  - Add helper `withRequestID(ctx context.Context, meta map[string]string) map[string]string` in `internal/audit/logger.go` that calls `requestid.FromContext(ctx)` and merges into metadata; omit key if absent
  - Call `withRequestID` inside `Logger.Log` so all audit paths (not just `LogAction`) propagate the ID
  - _Requirements: 5.1, 5.2, 5.3_

  - [ ]* 5.1 Write property test for audit metadata propagation (Property 7)
    - Generate random request IDs; inject into context; call `LogAction`; assert `MemorySink` entry metadata contains matching `"request_id"`
    - Also assert that when context has no ID, the key is absent from metadata
    - **Property 7: Audit entries carry request_id**
    - **Validates: Requirements 5.1, 5.2, 5.3**

- [x] 6. Propagate `job_id` and `worker_id` in the worker layer
  - Update `Worker.executeJob` in `internal/worker/worker.go` to call `requestid.WithJobID(ctx, job.ID)` and `requestid.WithWorkerID(ctx, w.config.WorkerID)` before passing context to `executor.Execute`
  - Update `Scheduler.ScheduleCharge/Invoice/Reminder` in `internal/worker/scheduler.go` to use `requestid.Generate()` for job IDs instead of `fmt.Sprintf("%s-%d", ...)`
  - Update all `zap.String("job_id", job.ID)` log calls in `worker.go` to also log `worker_id` from config
  - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5_

  - [ ]* 6.1 Write property tests for worker context propagation (Properties 8 & 11)
    - Use a mock executor that captures its context; generate random jobs; assert context contains `job_id` == `job.ID` and `worker_id` == `config.WorkerID`
    - Assert that on retry the same `job_id` is present (not regenerated)
    - **Property 8: Worker context carries job_id and worker_id**
    - **Property 11: Job IDs are stable across retries**
    - **Validates: Requirements 7.2, 7.3, 7.5**

  - [ ]* 6.2 Write property test for job ID uniqueness (Property from Req 7.1)
    - Generate N jobs via Scheduler; assert all IDs are distinct and pass `requestid.Sanitize`
    - **Validates: Requirements 7.1**

- [ ] 7. Wire `RequestIDWithConfig` into route registration
  - Update `internal/routes/routes.go` to call `middleware.RequestIDWithConfig(middleware.RequestIDConfig{TrustedProxies: cfg.RequestIDTrustedProxies})` as the first `r.Use(...)` call (before otelgin and rate limiting)
  - Remove any remaining call to `middleware.RequestLogger()` from route setup
  - _Requirements: 8.3, 8.4, 3.1_

- [-] 8. Checkpoint — ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for a faster MVP
- Property tests use `pgregory.net/rapid`; run with `go test -count=1 ./...`
- The `requestid.ContextKey` constant replaces all ad-hoc `"request_id"` string literals across packages
- Middleware registration order in `routes.go`: RequestID → otelgin → RateLimit → CORS → Auth
