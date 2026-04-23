# Requirements Document

## Introduction

This feature ensures every HTTP request and background job in the stellarbill-backend carries a consistent, traceable identifier (`request_id` / `job_id`) from entry point through to logs, traces, audit events, and error envelopes. The current codebase has two competing implementations (`middleware.RequestID()` in `middleware.go` and `middleware.RequestLogger()` in `logger.go`) that generate IDs independently and do not share a single source of truth. This spec consolidates them, adds trusted-source allowlisting, and extends propagation to the worker layer.

## Glossary

- **Request_ID_Middleware**: The Gin middleware responsible for extracting, validating, or generating the `request_id` for each HTTP request.
- **Job_ID**: A unique identifier attached to each background job at creation time and propagated through its execution context.
- **Trusted_Source**: A network peer (e.g., an internal API gateway or load balancer) whose inbound `X-Request-ID` header value is accepted without replacement.
- **Allowlist**: The configured set of trusted source IP prefixes or CIDR ranges whose inbound request IDs are accepted.
- **Sanitizer**: The function that validates and normalises an inbound request ID value before use.
- **Context_Key**: The string key used to store and retrieve the request/job ID from a `context.Context` or Gin context.
- **Error_Envelope**: The JSON response body returned on error, which must include the `request_id` field.
- **Audit_Entry**: A tamper-evident record written by the audit logger, which must include the `request_id` in its metadata.

---

## Requirements

### Requirement 1: Canonical Request ID Middleware

**User Story:** As a platform engineer, I want a single authoritative middleware that assigns a `request_id` to every HTTP request, so that all downstream components read from one consistent source.

#### Acceptance Criteria

1. THE Request_ID_Middleware SHALL assign a `request_id` value to the Gin context under the key `"request_id"` before any subsequent middleware or handler executes.
2. WHEN the inbound `X-Request-ID` header is absent or empty, THE Request_ID_Middleware SHALL generate a cryptographically random 24-hex-character ID.
3. WHEN the inbound `X-Request-ID` header is present and the request originates from a Trusted_Source, THE Request_ID_Middleware SHALL accept the header value after passing it through the Sanitizer.
4. WHEN the inbound `X-Request-ID` header is present and the request does not originate from a Trusted_Source, THE Request_ID_Middleware SHALL discard the header value and generate a new ID.
5. THE Request_ID_Middleware SHALL write the final `request_id` value to the `X-Request-ID` response header before the response is sent.
6. THE Request_ID_Middleware SHALL expose the `X-Request-ID` response header via `Access-Control-Expose-Headers` so browser clients can read it.

---

### Requirement 2: Request ID Sanitization

**User Story:** As a security engineer, I want all inbound request ID values to be validated before use, so that malformed or injection-attempt values cannot reach logs or downstream systems.

#### Acceptance Criteria

1. THE Sanitizer SHALL accept only values composed of ASCII alphanumeric characters and the characters `-`, `_`, and `.`.
2. THE Sanitizer SHALL reject any value whose length exceeds 128 characters.
3. THE Sanitizer SHALL reject any value that is empty or contains only whitespace.
4. IF the Sanitizer rejects a value, THEN THE Request_ID_Middleware SHALL treat the rejection as equivalent to an absent header and generate a new ID.
5. THE Sanitizer SHALL return the sanitized value unchanged when the value passes all checks.

---

### Requirement 3: Trusted Source Allowlisting

**User Story:** As a platform engineer, I want to configure which upstream peers are allowed to supply their own request IDs, so that arbitrary clients cannot spoof internal correlation identifiers.

#### Acceptance Criteria

1. THE Request_ID_Middleware SHALL read the Allowlist from the application configuration at startup.
2. WHEN the Allowlist is empty, THE Request_ID_Middleware SHALL treat every request as untrusted and always generate a new ID.
3. WHEN the Allowlist is non-empty, THE Request_ID_Middleware SHALL compare the request's remote IP address against each entry in the Allowlist using CIDR matching.
4. WHEN a remote IP matches an Allowlist entry, THE Request_ID_Middleware SHALL accept the sanitized inbound `X-Request-ID` value if present.
5. WHEN a remote IP does not match any Allowlist entry, THE Request_ID_Middleware SHALL discard any inbound `X-Request-ID` value and generate a new ID.
6. THE Request_ID_Middleware SHALL log a warning at DEBUG level when it discards a spoofed inbound ID.

---

### Requirement 4: Log Propagation

**User Story:** As an on-call engineer, I want every log line emitted during a request or job to include the `request_id` or `job_id`, so that I can filter all activity for a single request in one query.

#### Acceptance Criteria

1. WHEN a request completes, THE Request_ID_Middleware SHALL include the `request_id` field in the structured access log entry.
2. WHILE a request is being processed, THE Request_ID_Middleware SHALL make the `request_id` available on the `context.Context` so that any component that receives the context can attach it to log entries.
3. WHEN a background job begins execution, THE Worker SHALL attach the `job_id` to the job's execution `context.Context` under a well-known key.
4. WHEN a background job emits a log entry, THE Worker SHALL include the `job_id` field in that log entry.
5. WHEN a panic is recovered during request processing, THE Request_ID_Middleware SHALL include the `request_id` in the recovery log entry.

---

### Requirement 5: Audit Entry Propagation

**User Story:** As a compliance officer, I want every audit log entry to include the originating `request_id`, so that I can correlate audit events with the HTTP request that triggered them.

#### Acceptance Criteria

1. WHEN an audit entry is written during an HTTP request, THE Audit_Entry SHALL include the `request_id` in its `metadata` map under the key `"request_id"`.
2. THE Audit_Logger SHALL extract the `request_id` from the `context.Context` passed to its `Log` method.
3. IF no `request_id` is present in the context, THEN THE Audit_Logger SHALL omit the `"request_id"` metadata key rather than writing an empty value.

---

### Requirement 6: Error Envelope Propagation

**User Story:** As an API consumer, I want every error response to include the `request_id`, so that I can provide it to support when reporting issues.

#### Acceptance Criteria

1. WHEN a handler returns an HTTP error response (4xx or 5xx), THE Error_Envelope SHALL include a `"request_id"` field containing the current request's ID.
2. WHEN a panic is recovered, THE Error_Envelope SHALL include the `"request_id"` field in the 500 response body.
3. WHEN a rate-limit error is returned, THE Error_Envelope SHALL include the `"request_id"` field.
4. WHEN an authentication error is returned, THE Error_Envelope SHALL include the `"request_id"` field.

---

### Requirement 7: Worker Job ID Propagation

**User Story:** As a platform engineer, I want every background job to carry a stable `job_id` through its full lifecycle, so that I can trace a job's execution across retries and log entries.

#### Acceptance Criteria

1. WHEN a job is created by the Scheduler, THE Scheduler SHALL assign a unique `job_id` using a cryptographically random generator.
2. WHEN a job begins execution, THE Worker SHALL inject the `job_id` into the execution `context.Context` under the key `"job_id"`.
3. WHEN a job is retried, THE Worker SHALL reuse the original `job_id` rather than generating a new one.
4. WHEN a job is moved to the dead-letter queue, THE Worker SHALL include the `job_id` in the dead-letter log entry.
5. WHEN a job execution context is created, THE Worker SHALL also inject the `worker_id` into the context under the key `"worker_id"`.

---

### Requirement 8: Middleware Consolidation

**User Story:** As a developer, I want a single canonical request ID middleware used everywhere, so that there is no ambiguity about which middleware is active and IDs are never generated twice per request.

#### Acceptance Criteria

1. THE codebase SHALL contain exactly one active implementation of request ID assignment for HTTP requests.
2. WHEN `middleware.RequestLogger()` is replaced by the canonical Request_ID_Middleware, THE existing `RequestLogger` function SHALL be removed or clearly deprecated with a compile-time guard.
3. THE canonical Request_ID_Middleware SHALL be registered before all other middleware in the Gin engine setup.
4. THE `RequestIDKey` constant SHALL be the single source of truth for the context key name `"request_id"` across all packages.

---

### Requirement 9: Configuration

**User Story:** As a platform engineer, I want to configure request ID behaviour through environment variables, so that I can adjust trusted sources without recompiling.

#### Acceptance Criteria

1. THE Config SHALL support a `REQUEST_ID_TRUSTED_PROXIES` environment variable containing a comma-separated list of CIDR ranges.
2. WHEN `REQUEST_ID_TRUSTED_PROXIES` is not set, THE Config SHALL default to an empty Allowlist (untrusted mode).
3. WHEN `REQUEST_ID_TRUSTED_PROXIES` contains an invalid CIDR entry, THE Config SHALL log a warning and skip that entry rather than failing startup.
4. THE Config SHALL expose the parsed Allowlist as a `[]net.IPNet` slice on the `Config` struct.
