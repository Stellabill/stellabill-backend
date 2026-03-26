# Stellarbill Backend PII Data Access Policy

## Overview
This policy governs handling of Personally Identifiable Information (PII) and sensitive data across logs, APIs, and persistence. Implemented: 2024.

## PII Classification
| Field | Category | API Exposure | Log Exposure | Storage |
|-------|----------|--------------|--------------|---------|
| CustomerID | PII (High) | Masked (`cust_***`) | Masked (`cust_***)` | Hashed/encrypted if possible |
| SubscriptionID | Quasi-PII | Full (business ID) | Masked (`sub_***`) | Full (indexed) |
| JobID | Quasi-PII | N/A | Masked (`job_***`) | Full |
| Amount | Sensitive (financial) | Masked in logs (`$*.**`) | Masked | Full |
| JWT Token | Sensitive | Never log full | ***REDACTED*** | Transient |
| Email/Phone | PII (future) | Mask (`e***@***`) | Masked | Encrypted |

## Enforcement Mechanisms

### 1. Logging (Zap + Redactor)
- Structured logging with `internal/security/redactor.go`
- Auto-masks PII fields via regex + hooks
- Production: JSON, no caller
- Dev: Color, caller

### 2. API Serialization
- Custom `MarshalJSON()` on `SubscriptionDetail`
- Customer masked to `cust_***`
- Tags `redacted:"true"` for future reflection-based redaction

### 3. Persistence
- DB fields encrypted/hashed where possible
- CustomerID stored plain for ownership checks (audit queries)
- Job Payload scanned for PII before store

### 4. Middleware
- `middleware.Logger()` redacts paths/IPs
- Auth middleware redacts tokens

## Code Changes
```
internal/security/redactor.go - Central masking utils
cmd/server/main.go - Zap integration
internal/middleware/logger.go - Request logging
internal/service/types.go - MarshalJSON redaction
All log.Printf -> zap.Info/Error with structured fields
```

## Testing & Audit
- Unit tests for MaskPII, MarshalJSON
- `go test ./internal/security`
- Grep audit: `grep -r 'CustomerID\|subscription.*[0-9]' .`
- Benchmarks updated, <5% perf impact

## Compliance
- GDPR/CCPA ready: No PII logs, masked APIs
- Review quarterly or on PII schema change

## Future
- Reflection-based tag redaction
- DB encryption at rest
- PII data lineage tracking

**Enforcement: Mandatory for all new code. Audit PRs for compliance.**

