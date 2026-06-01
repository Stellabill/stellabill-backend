# 🛡️ Security Notes: Outbox & Audit Implementation (Issue #150)

## 1. Overview
This document defines the security architecture for the Stellabill backend. It integrates the Outbox Pattern with a tamper-evident Audit Logging system to ensure all sensitive operations—specifically admin tasks, reconciliation, and subscription mutations—are recorded with 100% accountability.

---

## 2. Immutable Audit Trail (Issue #150 Requirement)
To satisfy the requirement for "immutable, tamper-evident records," we implement **HMAC-SHA256 Chaining**. This creates a cryptographic link between all historical logs.

### 2.1 The Hashing Mechanism
Each log entry contains a `Hash` and a `PrevHash`.
- **Logic**: The `Hash` of entry $N$ is calculated using the payload of entry $N$ plus the `Hash` of entry $N-1$.
- **Implication**: Any unauthorized `UPDATE` or `DELETE` in the database will break the chain. A background validator confirms the integrity of the chain by re-calculating hashes using the system's private secret.

### 2.2 Traceability (RequestID)
Correlation across the distributed system is handled via `RequestID`.
- **Extraction**: The `AuditMiddleware` extracts the `X-Request-ID` from the incoming HTTP headers.
- **Persistence**: This ID is stored in both the **Audit Log** and the **Outbox Event** table, allowing security teams to trace an external event back to the specific internal actor and request context.

---

## 3. Data Security & PII Protection

### 3.1 Mandatory Redaction
Before any data is persisted to the `outbox_events` or `audit_logs` tables, it must pass through a redaction filter. 
- **Blacklisted Keys**: `password`, `token`, `secret`, `auth_key`, `cvv`, `mnemonic`.
- **Strategy**: Sensitive values are replaced with `[REDACTED]` at the application layer to ensure PII is never stored in plaintext or backups.

### 3.2 Database Security
- **Least Privilege**: The application database user is granted `SELECT`, `INSERT`, and `UPDATE` permissions. `DELETE` permissions are strictly denied to prevent the removal of audit trails.
- **Encryption at Rest**: All event data must be stored on AES-256 encrypted volumes (TDE) to protect against physical data breaches.

---

## 4. Application & Network Security

### 4.1 HTTPS/TLS Enforcement
- **Requirement**: All event publishers must utilize **TLS 1.2** or higher.
- **Verification**: Certificate pinning is utilized for critical endpoints. The use of `InsecureSkipVerify` in Go publishers is strictly prohibited and will fail security audits.

### 4.2 Failure Path Coverage
To meet Issue #150 compliance, events must be emitted even during failures.
- **Logic**: If a reconciliation process fails, an `AuditEvent` is emitted with `Outcome: failure` and the sanitized error reason. This ensures that "hidden" failures cannot be used to mask malicious activity.

---

## 5. Compliance & Testing

### 5.1 95% Test Coverage Requirement
This implementation is governed by a strict coverage mandate. 
- **Packages**: `internal/audit` and `internal/outbox`.
- **Verification**: `go test -v -coverprofile=cover.out ./...`.
- **Requirement**: PRs will only be merged if total coverage for these security packages exceeds **95%**.

### 5.2 Audit Checklist
- [ ] HMAC Chain valid (PrevHash matches previous Hash).
- [ ] PII scrubbed from Metadata.
- [ ] RequestID present in all log entries.
- [ ] Failure paths covered in unit tests.

---

## 6. Threat Model & Incident Response

### Common Attack Vectors
1. **Replay Attacks**: Prevented by the `RequestID` and `Timestamp` idempotency checks in the Outbox relay.
2. **Data Injection**: Prevented by strict schema validation before event creation.
3. **Log Tampering**: Prevented by the HMAC-SHA256 chain.

### Response Procedures
In the event of a detected **Hash Mismatch**:
1. Isolate the database partition.
2. Cross-reference the broken chain against off-site, read-only S3 backups.
3. Identify the `Actor` associated with the last valid hash to begin root cause analysis.

---
