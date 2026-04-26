# Stellabill Backend — Operational Runbooks

This directory contains incident response runbooks for the Stellabill backend service. Each runbook includes alert thresholds, triage checklists, log queries, dashboard links, and step-by-step mitigation procedures.

---

## Runbooks

| Runbook | Failure Mode | Pager threshold |
|---------|-------------|-----------------|
| [auth-failure-runbook.md](./auth-failure-runbook.md) | JWT validation failures, tenant mismatches, admin token errors | 401 rate > 10 % in 5 min |
| [db-outage-runbook.md](./db-outage-runbook.md) | PostgreSQL outages, connection pool exhaustion, replica lag, slow queries | Health check `"db": "down"` for > 2 min |
| [elevated-errors-runbook.md](./elevated-errors-runbook.md) | 5xx spike, panics, worker failures, latency degradation | 5xx rate > 5 % in 5 min |

---

## Alert Threshold Quick Reference

### Authentication Failures

| Threshold | Warning | Critical |
|-----------|---------|---------|
| 401 rate (5 min window) | > 2 % of requests | > 10 % of requests |
| 401 spike | — | 5× baseline in < 2 min |
| Tenant mismatch rate | > 1 % | > 5 % |
| Admin endpoint 401s | — | > 5 in 1 min |

### Database Outages

| Threshold | Warning | Critical |
|-----------|---------|---------|
| Connection errors | > 5 /min | > 20 /min |
| Connection pool | — | < 10 % available |
| p99 query latency | > 500 ms | > 2 000 ms |
| Health check `db: down` | — | > 2 min |
| Replication lag | > 30 s | > 5 min |

### Elevated Error Rates

| Threshold | Warning | Critical |
|-----------|---------|---------|
| 5xx rate (5 min window) | > 1 % | > 5 % (> 25 % = emergency) |
| Panic rate (1 min) | > 10 /min | > 25 /min |
| p99 latency | — | > 3 000 ms |
| Worker failures | > 5 in 5 min | > 25 in 5 min |

---

## Incident Response Framework

All incidents follow five phases:

1. **Detect** — alert fires or manual observation
2. **Assess** — triage checklist determines scope and severity
3. **Mitigate** — apply the fastest fix (rollback, restart, feature flag)
4. **Recover** — verify all subsystems healthy via `/api/health` and endpoint smoke tests
5. **Post-incident** — root cause analysis, threshold calibration, test coverage

---

## Escalation Contacts

| Role | When to escalate |
|------|-----------------|
| On-call engineer | All Warning and Critical alerts |
| Backend team lead | Persistent Critical after 30 min, or code-level root cause |
| DBA / Infrastructure | PostgreSQL won't start, disk full, OOM |
| Security team | Credential leak, suspected breach, data cross-contamination |
| Engineering manager | > 30 min at Critical with no resolution path |

---

## Security Reminders

- **Never log** `DATABASE_URL`, `JWT_SECRET`, `ADMIN_TOKEN`, or raw `Authorization` headers.  
  The audit logging and panic recovery middleware already redacts these. If you find them in logs, treat it as a security incident and rotate credentials immediately.
- **Never instruct clients** to disable TLS or send credentials in query parameters as a workaround.
- Temporary auth bypasses (§6.4 of the auth runbook) require on-call lead approval and must be reverted within 4 hours.

---

## Related Documentation

- [`docs/security-notes.md`](../security-notes.md) — Security guidelines and threat model
- [`docs/outbox-pattern.md`](../outbox-pattern.md) — Event publishing and reliability
- [`docs/panic-recovery.md`](../panic-recovery.md) — Panic recovery middleware
- [`docs/RATE_LIMITING.md`](../RATE_LIMITING.md) — Rate limiting configuration
- [`docs/ERROR_ENVELOPE.md`](../ERROR_ENVELOPE.md) — Standardized error response format</content>
<parameter name="filePath">/workspaces/stellabill-backend/docs/ops/README.md