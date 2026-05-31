# Audit Middleware Implementation

## Overview

This document describes the implementation of audit logging middleware that captures authentication failures (401/403) and admin actions throughout the stellabill-backend application.

## Changes Made

### 1. Configuration (`internal/config/config.go`)

Added `AuditLogPath` field to the `Config` struct to support configurable audit log destination:

```go
// Audit configuration
AuditLogPath string
```

The configuration reads from the `AUDIT_LOG_PATH` environment variable with an empty string as default (falls back to stderr).

### 2. Audit Sink (`internal/audit/sink.go`)

Added `StderrSink` implementation for fallback when no file path is configured:

```go
type StderrSink struct {
	mu sync.Mutex
}

func NewStderrSink() *StderrSink
func (s *StderrSink) WriteEvent(e AuditEvent) error
```

**Behavior:**
- Writes JSONL (JSON Lines) format to `os.Stderr`
- Thread-safe with mutex protection
- Non-blocking: write failures do not break the request

### 3. Routes Wiring (`internal/routes/routes.go`)

#### Audit Logger Construction

```go
// Configure audit logging
var auditSink audit.Sink
if cfg.AuditLogPath != "" {
	auditSink = audit.NewFileSink(cfg.AuditLogPath)
} else {
	auditSink = audit.NewStderrSink()
}
auditSecret := os.Getenv("AUDIT_SECRET")
if auditSecret == "" {
	auditSecret = jwtSecret // Fallback to JWT secret for dev
}
auditLogger := audit.NewLogger(auditSecret, auditSink)
```

**Logic:**
- If `AUDIT_LOG_PATH` is set → use `FileSink`
- If `AUDIT_LOG_PATH` is empty → use `StderrSink`
- Uses `AUDIT_SECRET` for HMAC chaining, falls back to `JWT_SECRET` in dev

#### Middleware Installation

The audit middleware is installed on all protected route groups:

```go
// V1 routes
v1.Use(authMiddleware)
v1.Use(audit.Middleware(auditLogger))

// Legacy /api routes
apiProtected.Use(authMiddleware)
apiProtected.Use(audit.Middleware(auditLogger))

// Admin routes
admin.Use(authMiddleware)
admin.Use(audit.Middleware(auditLogger))
```

**Placement:** Audit middleware is installed **after** auth middleware to ensure:
1. Auth failures (401/403) are captured automatically
2. Actor information from auth context is available
3. Request context has been enriched with user identity

### 4. Admin Handler (`internal/handlers/admin.go`)

No changes required. The existing `PurgeCache` handler already calls `audit.LogAction`:

```go
audit.LogAction(c, "admin_purge", target, auditOutcome, map[string]string{
	"attempt":     attempt,
	"keys_purged": strconv.Itoa(totalKeys),
})
```

### 5. Reconciliation Handler (`internal/handlers/reconciliation.go`)

Added audit logging to `NewReconcileHandler`:

```go
// Audit log the reconciliation action
outcome := "success"
if matched < len(reports) {
	outcome = "partial"
}
audit.LogAction(c, "reconciliation.execute", "reconciliation", outcome, map[string]string{
	"total":      strconv.Itoa(len(reports)),
	"matched":    strconv.Itoa(matched),
	"mismatched": strconv.Itoa(len(reports) - matched),
	"tenant_id":  tenantID,
})
```

**Captured Data:**
- Action: `reconciliation.execute`
- Outcome: `success` (all matched) or `partial` (some mismatches)
- Metadata: total, matched, mismatched counts, tenant_id

## Security Features

### 1. PII Redaction

The audit logger automatically redacts sensitive fields:
- `password`, `token`, `secret`, `auth`, `key`, `cvv`, `card`
- Bearer tokens in Authorization headers
- Redacted value: `***REDACTED***`

### 2. Cryptographic Chaining

Each audit event includes:
- `prev_hash`: HMAC-SHA256 of previous event
- `hash`: HMAC-SHA256 of current event
- Secret: `AUDIT_SECRET` (or `JWT_SECRET` fallback)

This creates a tamper-evident chain where any modification breaks the hash chain.

### 3. Non-Blocking Writes

Audit sink write failures do not break the request:
- `LogAction` silently returns if logger is not available
- Middleware continues processing even if audit write fails
- Errors are logged but not propagated to the client

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `AUDIT_LOG_PATH` | No | `""` (stderr) | File path for audit log (JSONL format) |
| `AUDIT_SECRET` | No | `JWT_SECRET` | HMAC secret for event chaining |

### Example Configuration

```bash
# Write to file
AUDIT_LOG_PATH=/var/log/stellabill/audit.log

# Write to stderr (default)
# AUDIT_LOG_PATH=

# Custom HMAC secret (recommended for production)
AUDIT_SECRET=your-strong-secret-here
```

## Audit Event Format

Events are written in JSON Lines format (one JSON object per line):

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "request_id": "req-123",
  "actor": "user@example.com",
  "action": "admin_purge",
  "resource": "billing-cache",
  "outcome": "success",
  "prev_hash": "abc123...",
  "hash": "def456...",
  "metadata": {
    "keys_purged": "10",
    "path": "/api/admin/purge",
    "method": "POST",
    "client_ip": "192.168.1.1"
  }
}
```

## Captured Events

### 1. Authentication Failures

**Trigger:** Any response with status 401 or 403

**Event:**
```json
{
  "action": "auth_failure",
  "resource": "/api/v1/subscriptions",
  "outcome": "status_401",
  "metadata": {
    "path": "/api/v1/subscriptions",
    "method": "GET",
    "status": "401",
    "auth_header": "***REDACTED***"
  }
}
```

### 2. Admin Cache Purge

**Trigger:** `POST /api/admin/purge`

**Event:**
```json
{
  "action": "admin_purge",
  "resource": "billing-cache",
  "outcome": "success",
  "metadata": {
    "attempt": "1",
    "keys_purged": "10"
  }
}
```

### 3. Reconciliation Execution

**Trigger:** `POST /api/admin/reconcile`

**Event:**
```json
{
  "action": "reconciliation.execute",
  "resource": "reconciliation",
  "outcome": "success",
  "metadata": {
    "total": "5",
    "matched": "5",
    "mismatched": "0",
    "tenant_id": "tenant-123"
  }
}
```

## Testing

### Test Coverage

The implementation includes comprehensive tests in `internal/routes/routes_audit_test.go`:

1. **Middleware Wiring Tests**
   - `TestAuditMiddlewareWiring/auth_failure_401_logged`
   - `TestAuditMiddlewareWiring/auth_failure_403_logged`
   - `TestAuditMiddlewareWiring/admin_purge_logged`
   - `TestAuditMiddlewareWiring/reconciliation_logged`

2. **Sink Fallback Tests**
   - `TestAuditSinkFallback/stderr_sink_does_not_break_request`
   - `TestAuditSinkFallback/file_sink_write_failure_does_not_break_request`

3. **PII Redaction Tests**
   - `TestAuditPIIRedaction/auth_header_redacted`
   - `TestAuditPIIRedaction/password_metadata_redacted`

4. **Configuration Tests**
   - `TestAuditConfigFromEnv/audit_log_path_from_env`
   - `TestAuditConfigFromEnv/audit_log_path_empty_uses_stderr`

5. **Chaining Tests**
   - `TestAuditChaining/events_are_chained`

6. **File Sink Tests**
   - `TestFileSinkCreatesFile/file_sink_creates_file`
   - `TestFileSinkCreatesFile/file_sink_appends`

7. **Stderr Sink Tests**
   - `TestStderrSinkWrites/stderr_sink_writes`

### Running Tests

```bash
# Run all audit tests
go test ./internal/audit/... -v

# Run routes audit tests
go test ./internal/routes/... -run TestAudit -v

# Run with coverage
go test ./internal/audit/... -cover
go test ./internal/routes/... -run TestAudit -cover

# Generate coverage report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Expected Coverage

- Audit package: >95%
- Routes audit integration: >90%
- Overall: >95%

## Edge Cases Handled

### 1. Missing AUDIT_LOG_PATH

**Behavior:** Falls back to `StderrSink`
**Test:** `TestAuditConfigFromEnv/audit_log_path_empty_uses_stderr`

### 2. Sink Write Failure

**Behavior:** Request continues successfully, error is silently logged
**Test:** `TestAuditSinkFallback/file_sink_write_failure_does_not_break_request`

### 3. PII in Metadata

**Behavior:** Sensitive fields are automatically redacted
**Test:** `TestAuditPIIRedaction/password_metadata_redacted`

### 4. Missing Audit Logger

**Behavior:** `LogAction` returns early without error
**Implementation:** Check in `audit.LogAction`:
```go
raw, ok := c.Get(loggerContextKey)
if !ok {
	return
}
```

### 5. Concurrent Writes

**Behavior:** Both `FileSink` and `StderrSink` use mutex for thread safety
**Implementation:** `mu sync.Mutex` in both sink types

## Deployment Checklist

- [ ] Set `AUDIT_LOG_PATH` in production environment
- [ ] Set `AUDIT_SECRET` distinct from `JWT_SECRET`
- [ ] Ensure audit log directory exists and is writable
- [ ] Configure log rotation (e.g., logrotate)
- [ ] Set up monitoring for audit log disk usage
- [ ] Verify audit events are being written (check file or stderr)
- [ ] Test auth failure logging (401/403)
- [ ] Test admin action logging (purge, reconcile)
- [ ] Verify PII redaction is working
- [ ] Confirm hash chaining is intact

## Monitoring

### Key Metrics

1. **Audit Event Rate**
   - Monitor events/second
   - Alert on sudden drops (may indicate logging failure)

2. **Disk Usage**
   - Monitor audit log file size
   - Set up rotation before disk fills

3. **Write Failures**
   - Monitor stderr for write errors
   - Alert on repeated failures

### Log Rotation

Example logrotate configuration:

```
/var/log/stellabill/audit.log {
    daily
    rotate 90
    compress
    delaycompress
    notifempty
    create 0600 stellabill stellabill
    postrotate
        systemctl reload stellabill-backend
    endscript
}
```

## Troubleshooting

### No Audit Events

1. Check `AUDIT_LOG_PATH` is set correctly
2. Verify file permissions (should be writable)
3. Check stderr output if using default sink
4. Verify middleware is installed (check routes.go)

### Redacted Fields Not Working

1. Check field names match sensitive patterns
2. Verify logger is initialized with secret
3. Check metadata is passed as `map[string]string`

### Hash Chain Broken

1. Verify `AUDIT_SECRET` hasn't changed
2. Check for concurrent logger instances
3. Ensure single logger instance per application

## Future Enhancements

1. **Structured Logging Integration**
   - Send audit events to centralized logging (e.g., ELK, Splunk)
   - Add correlation IDs for distributed tracing

2. **Audit Query API**
   - Endpoint to query audit logs
   - Filter by actor, action, time range

3. **Real-time Alerting**
   - Alert on suspicious patterns (e.g., repeated auth failures)
   - Anomaly detection for admin actions

4. **Compliance Reports**
   - Generate compliance reports (SOC2, GDPR)
   - Export audit trail for auditors

## References

- Audit middleware: `internal/audit/middleware.go`
- Audit logger: `internal/audit/logger.go`
- Audit sinks: `internal/audit/sink.go`
- Routes wiring: `internal/routes/routes.go`
- Admin handler: `internal/handlers/admin.go`
- Reconciliation handler: `internal/handlers/reconciliation.go`
- Tests: `internal/routes/routes_audit_test.go`
