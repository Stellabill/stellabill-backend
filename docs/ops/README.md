# Stellabill Backend — Operational Runbooks

This directory contains incident response runbooks for the Stellabill backend service. Each runbook includes alert thresholds, triage checklists, log queries, dashboard links, and step-by-step mitigation procedures.

---

## Runbooks

| Runbook | Failure Mode | Pager threshold |
|---------|-------------|-----------------|
| [auth-failure-runbook.md](./auth-failure-runbook.md) | JWT validation failures, tenant mismatches, admin token errors | 401 rate > 10 % in 5 min |
| [db-outage-runbook.md](./db-outage-runbook.md) | PostgreSQL outages, connection pool exhaustion, replica lag, slow queries | Health check `"db": "down"` for > 2 min |
| [elevated-errors-runbook.md](./elevated-errors-runbook.md) | 5xx spike, panics, worker failures, latency degradation | 5xx rate > 5 % in 5 min |
| [outbox-backlog.md](./runbooks/outbox-backlog.md) | Outbox event backlog growth, publisher lag, DLQ spikes, dispatcher stalls | `outbox_backlog_depth` > 2,500 for 5 min |
| [keda-worker-autoscale.md](./keda-worker-autoscale.md) | KEDA scales workers from `outbox_backlog_depth` (1..20 replicas) | Idle backlog → `minReplicaCount=1` |

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

### Outbox Backlog & Publisher Lag

| Threshold | Warning | Critical |
|-----------|---------|---------|
| Backlog depth (`outbox_backlog_depth`) | > 500 events | > 2,500 events |
| Publisher lag (`outbox_publisher_lag_seconds`) | > 60 s | > 300 s |
| Dead-letter inflow (`dead_letter_events`) | — | >= 5 in 1 min |
| Dispatcher status | — | Stalled / Stopped |
| KEDA worker scale (see [keda-worker-autoscale.md](./keda-worker-autoscale.md)) | target 100 events/replica | max 20 replicas; idle → 1 |

---

## Incident Response Framework

All incidents follow five phases:

1. **Detect** — alert fires or manual observation
2. **Assess** — triage checklist determines scope and severity
3. **Mitigate** — apply the fastest fix (rollback, restart, feature flag)
4. **Recover** — verify all subsystems healthy via `/api/health` and endpoint smoke tests
5. **Post-incident** — root cause analysis, threshold calibration, test coverage

After mitigation and recovery, file a blameless postmortem using the [postmortem template](./postmortem-template.md) and add it to the [postmortem index](./postmortems/README.md). Security and cost incidents use the same template (see **Incident type** and **Non-outage impact** sections).

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

- [`postmortem-template.md`](./postmortem-template.md) — Blameless postmortem template
- [`postmortems/README.md`](./postmortems/README.md) — Index of filed postmortems
- [`docs/security-notes.md`](../security-notes.md) — Security guidelines and threat model
- [`docs/outbox-pattern.md`](../outbox-pattern.md) — Event publishing and reliability
- [`docs/panic-recovery.md`](../panic-recovery.md) — Panic recovery middleware
- [`docs/RATE_LIMITING.md`](../RATE_LIMITING.md) — Rate limiting configuration
- [`docs/ERROR_ENVELOPE.md`](../ERROR_ENVELOPE.md) — Standardized error response format