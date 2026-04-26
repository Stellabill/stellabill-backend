# Authentication Failure Runbook

**Service:** Stellabill Backend (Go/Gin)
**Owner:** On-call engineer
**Last updated:** 2026-04-23
**Related docs:** [`../security-notes.md`](../security-notes.md), [`../ERROR_ENVELOPE.md`](../ERROR_ENVELOPE.md)

---

## 1. Overview

This runbook covers JWT token validation and authorization failures in the Stellabill backend. The service uses JWT-based authentication with tenant isolation via the `X-Tenant-ID` header. All auth failures return **HTTP 401** with a JSON `ErrorEnvelope` body:

```json
{
  "code": "UNAUTHORIZED",
  "message": "<specific reason>",
  "trace_id": "<uuid>"
}
```

The `trace_id` field correlates every auth failure to a specific request across all log sources.

---

## 2. Alert Thresholds

| Alert | Condition | Severity | Pager? | Response SLA |
|-------|-----------|----------|--------|--------------|
| `auth_failure_rate_warning` | 401 responses > **2 %** of total requests | ⚠️ Warning | No | 30 min |
| `auth_failure_rate_critical` | 401 responses > **10 %** of total requests | 🔴 Critical | Yes | 15 min |
| `auth_failure_spike` | 401 count increases **5× baseline** in < 2 min | 🔴 Critical | Yes | 10 min |
| `tenant_mismatch_warning` | Tenant mismatch errors > **1 %** of auth attempts | ⚠️ Warning | No | 30 min |
| `tenant_mismatch_critical` | Tenant mismatch errors > **5 %** of auth attempts | 🔴 Critical | Yes | 15 min |
| `admin_token_failures` | Admin endpoint 401s > **5** in 1 min | 🔴 Critical | Yes | 5 min |

> **Baseline:** Average 401 rate over previous 7 days at the same hour (same-hour rolling baseline).

---

## 3. What to Check First (Triage Checklist)

Run through this list **in order**. Stop at the first finding and jump to the relevant section.

- [ ] **1. Is the JWT secret misconfigured or recently rotated?**  
  Check whether `JWT_SECRET` was changed in the last 24 hours (deployment logs, secret manager audit trail). A secret rotation without rolling all pods causes all existing tokens to become invalid simultaneously.

- [ ] **2. Are failures from a single tenant or across all tenants?**  
  A single-tenant spike suggests a client-side issue (bad token, clock skew). A cross-tenant spike suggests a secret or middleware problem.

- [ ] **3. Is the failure pattern sudden or gradual?**  
  Sudden = secret rotation, deployment, or bad release. Gradual = token TTL drift, client library bug, or clock skew.

- [ ] **4. Are admin endpoints affected?**  
  Admin token failures are higher severity — check `ADMIN_TOKEN` separately from `JWT_SECRET`.

- [ ] **5. Is there an elevated 429 rate alongside the 401s?**  
  Rate limiter abuse (bots probing credentials) can manifest as correlated 401+429 spikes.

---

## 4. Log Queries

All logs are JSON-structured. Adjust the time range (`--since`) as needed.

### 4.1 Count 401s by error message (last 30 min)

```bash
journalctl -u stellabill-backend --since "30 minutes ago" --no-pager -o json \
  | jq -r 'select(.status == 401) | .message' \
  | sort | uniq -c | sort -rn
```

### 4.2 Isolate tenant mismatch errors

```bash
journalctl -u stellabill-backend --since "1 hour ago" --no-pager -o json \
  | jq -r 'select(.message == "tenant mismatch") | {time: .REALTIME_TIMESTAMP, tenant: .tenant_id, trace: .trace_id}'
```

### 4.3 Find all 401s with trace IDs (for cross-service correlation)

```bash
journalctl -u stellabill-backend --since "1 hour ago" --no-pager -o json \
  | jq -r 'select(.status == 401) | [.REALTIME_TIMESTAMP, .trace_id, .message] | @tsv'
```

### 4.4 Check for admin token failures specifically

```bash
journalctl -u stellabill-backend --since "1 hour ago" --no-pager -o json \
  | jq -r 'select(.path | test("/admin/")) | select(.status == 401)'
```

> **Security note:** Never log the raw `Authorization` header, `JWT_SECRET`, or `ADMIN_TOKEN`. The audit logging middleware already redacts these.

---

## 5. Dashboard Links

| Dashboard | Purpose |
|-----------|---------|
| `https://grafana.internal/d/auth-overview` | 401 rate, breakdown by error type and tenant |
| `https://grafana.internal/d/request-overview` | Overall HTTP status code distribution |
| `https://grafana.internal/explore?query=status%3D401` | Live log explorer filtered to 401s |
| `https://grafana.internal/alerts` | Active alert list |

> If Grafana is unavailable, use the log queries in §4 directly on the host.

---

## 6. Mitigation Steps

### 6.1 JWT secret mismatch after rotation

```bash
# Confirm currently loaded secret hash (do NOT print the value)
kubectl exec -it deploy/stellabill-backend -- sh -c 'echo $JWT_SECRET | sha256sum'

# Compare with the expected hash from your secrets manager
# If mismatched, trigger a rolling restart to pick up the new secret
kubectl rollout restart deployment/stellabill-backend
kubectl rollout status deployment/stellabill-backend
```

### 6.2 Clock skew causing token expiry

```bash
# Check system clock on API hosts
timedatectl status

# If NTP is drifted, sync immediately
systemctl restart systemd-timesyncd
timedatectl timesync-status
```

### 6.3 Tenant configuration broken

```bash
# Confirm tenant header validation is enabled (should be "true")
kubectl exec -it deploy/stellabill-backend -- sh -c 'echo $TENANT_VALIDATION_ENABLED'

# Review recent tenant config changes in deployment history
kubectl rollout history deployment/stellabill-backend
```

### 6.4 Temporary bypass for critical endpoints (last resort)

**Requires approval from on-call lead.**

```bash
# Enable bypass for specific path prefix only — document the reason
kubectl set env deployment/stellabill-backend AUTH_BYPASS_PATHS="/api/health,/api/billing/emergency"
# IMPORTANT: Revert within 4 hours — set a calendar reminder now
```

---

## 7. Verification & Recovery

After applying a fix, confirm recovery with all three checks:

```bash
# 1. Health endpoint (should return 200)
curl -sf https://api.stellabill.internal/api/health | jq .

# 2. Valid auth attempt (should return 200, not 401)
curl -sf -H "Authorization: Bearer $TEST_TOKEN" \
     -H "X-Tenant-ID: test-tenant" \
     https://api.stellabill.internal/api/subscriptions | jq .status

# 3. 401 rate should be falling
journalctl -u stellabill-backend --since "5 minutes ago" --no-pager -o json \
  | jq -r 'select(.status == 401)' | wc -l
```

Declare recovery when the 401 rate drops below **1 %** and stays there for **10 consecutive minutes**.

---

## 8. Escalation

| Condition | Escalate to |
|-----------|-------------|
| JWT secret confirmed correct but failures persist | Backend team lead |
| Suspected credential theft / brute force | Security team (immediate) |
| Tenant data cross-contamination suspected | Backend lead + Data team |
| > 30 min at Critical severity with no fix | Engineering manager |

---

## 9. Post-Incident Checklist

- [ ] Root cause documented in incident tracker
- [ ] JWT rotation procedure reviewed — is it fully automated with zero-downtime rolling?
- [ ] Alert thresholds calibrated against actual baseline (§2)
- [ ] Confirm no secrets were written to logs during investigation (audit log review)
- [ ] Update `docs/security-notes.md` if new security finding discovered
- [ ] Add/update multi-tenant auth tests covering the failure mode</content>
<parameter name="filePath">/workspaces/stellabill-backend/docs/ops/auth-failure-runbook.md