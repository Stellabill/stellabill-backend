# Runbook: Elevated Error Rates

**Service:** Stellabill Backend (Go/Gin)
**Owner:** On-call engineer
**Last updated:** 2026-04-23
**Related docs:** [`docs/panic-recovery.md`](../panic-recovery.md), [`docs/RATE_LIMITING.md`](../RATE_LIMITING.md), [`docs/ERROR_ENVELOPE.md`](../ERROR_ENVELOPE.md)

---

## 1. Overview

This runbook covers incidents where the Stellabill API's 5xx error rate, panic rate, or worker failure rate rises above acceptable levels. All errors return a JSON `ErrorEnvelope`:

```json
{
  "code": "INTERNAL_ERROR",
  "message": "internal error",
  "trace_id": "<uuid>"
}
```

The `trace_id` links log entries across the API, worker, and any upstream services.

---

## 2. Alert Thresholds

### 2.1 HTTP Error Rate (5-minute sliding window)

| Alert | Condition | Severity | Pager? | Response SLA |
|-------|-----------|----------|--------|--------------|
| `error_rate_warning` | 5xx rate > **1 %** of total requests | ⚠️ Warning | No | 30 min |
| `error_rate_critical` | 5xx rate > **5 %** of total requests | 🔴 Critical | Yes | 10 min |
| `error_rate_emergency` | 5xx rate > **25 %** of total requests | 🔴 Critical | Yes | 5 min |
| `error_spike` | 5xx count increases **10× baseline** in < 3 min | 🔴 Critical | Yes | 5 min |

### 2.2 Panic Rate (1-minute window)

| Alert | Condition | Severity | Pager? | Response SLA |
|-------|-----------|----------|--------|--------------|
| `panic_warning` | Panics > **10** per minute | ⚠️ Warning | No | 20 min |
| `panic_critical` | Panics > **25** per minute | 🔴 Critical | Yes | 5 min |
| `panic_sudden` | Any panics after **0 panics** in previous hour | ⚠️ Warning | No | 20 min |

### 2.3 Latency (5-minute window)

| Alert | Condition | Severity | Pager? | Response SLA |
|-------|-----------|----------|--------|--------------|
| `latency_warning` | p95 latency > **800 ms** | ⚠️ Warning | No | 30 min |
| `latency_critical` | p99 latency > **3 000 ms** | 🔴 Critical | Yes | 15 min |

### 2.4 Worker (5-minute window)

| Alert | Condition | Severity | Pager? | Response SLA |
|-------|-----------|----------|--------|--------------|
| `worker_failure_warning` | Worker job failures > **5** per 5 min | ⚠️ Warning | No | 30 min |
| `worker_failure_critical` | Worker job failures > **25** per 5 min | 🔴 Critical | Yes | 10 min |
| `worker_down` | Worker reports `"worker": "stopped"` for > 2 min | 🔴 Critical | Yes | 5 min |

> **Baseline definition:** rolling 7-day same-hour average for the same endpoint group.

---

## 3. What to Check First (Triage Checklist)

Run through this list **in order**.

- [ ] **1. Is the service still responding?**  
  Hit `/api/health`. A 5xx there means the service is severely degraded. A 200 with a bad `db` or `worker` field narrows scope.

- [ ] **2. Which endpoints are erroring?**  
  A single endpoint erroring (e.g., `/api/subscriptions`) points to a code/data bug. All endpoints erroring points to infrastructure (DB, OOM, config).

- [ ] **3. Did a deployment happen in the last 30 minutes?**  
  A new release is the most common cause of sudden error spikes. Rollback is often the fastest fix.

- [ ] **4. Are panics involved?**  
  Panics recovered by the middleware still return 500s. Panic logs include full stack traces — check for them before assuming a logic error.

- [ ] **5. Is the DB healthy?**  
  DB errors cascade into 500s across all endpoints. Check `/api/health` and the DB outage runbook before digging into application code.

- [ ] **6. Are external dependencies failing?**  
  Billing parsers, external HTTP calls, or the outbox publisher can cause cascading 500s. Check circuit breaker status.

- [ ] **7. Are resources (memory, CPU, disk) saturated?**  
  OOM kills and CPU saturation both manifest as elevated 5xxs. Check resource metrics before log-diving.

---

## 4. Log Queries

### 4.1 5xx error count and breakdown by endpoint (last 30 min)

```bash
journalctl -u stellabill-backend --since "30 minutes ago" --no-pager -o json \
  | jq -r 'select(.status >= 500) | {path: .path, status: .status, message: .message}' \
  | jq -s 'group_by(.path) | map({path: .[0].path, count: length}) | sort_by(-.count)'
```

### 4.2 Find all panic traces (last 1 hour)

```bash
journalctl -u stellabill-backend --since "1 hour ago" --no-pager -o json \
  | jq -r 'select(.panic == true or (.message | test("panic|recovered"))) | {time: .REALTIME_TIMESTAMP, trace: .trace_id, stack: .stack_trace}'
```

### 4.3 Worker failure log entries

```bash
journalctl -u stellabill-worker --since "30 minutes ago" --no-pager -o json \
  | jq -r 'select(.level == "error") | {time: .REALTIME_TIMESTAMP, job: .job_type, error: .error, trace: .trace_id}'
```

### 4.4 Error rate per minute (quick trend)

```bash
journalctl -u stellabill-backend --since "30 minutes ago" --no-pager -o json \
  | jq -r 'select(.status >= 500) | .REALTIME_TIMESTAMP[:16]' \
  | sort | uniq -c
```

### 4.5 Identify specific error codes (billing parse errors, etc.)

```bash
journalctl -u stellabill-backend --since "1 hour ago" --no-pager -o json \
  | jq -r 'select(.level == "error") | .message' \
  | sort | uniq -c | sort -rn | head -20
```

> **Security note:** Never add debug logging that includes request bodies, authorization headers, or tenant data. The panic recovery middleware intentionally withholds stack traces from HTTP responses — do not bypass this in production.

---

## 5. Diagnostic Commands

```bash
# 1. Health check
curl -sf https://api.stellabill.internal/api/health | jq .

# 2. Resource usage on API hosts
top -bn1 | head -20
free -h
df -h /

# 3. Is the process OOM-killed? (look for "OOM" or "killed" in kernel log)
sudo dmesg | grep -i "oom\|killed" | tail -20

# 4. Open file descriptors (high count = file/socket leak)
ls /proc/$(pgrep stellabill)/fd | wc -l

# 5. Active goroutines via the debug endpoint (if enabled in staging)
curl -sf https://api.stellabill.internal/debug/pprof/goroutine?debug=1 | head -50

# 6. Circuit breaker status (if instrumented)
curl -sf https://api.stellabill.internal/api/health | jq .circuit_breakers
```

---

## 6. Dashboard Links

| Dashboard | Purpose |
|-----------|---------|
| `https://grafana.internal/d/error-overview` | 5xx rate, breakdown by endpoint, panic rate |
| `https://grafana.internal/d/latency` | p50/p95/p99 latency per endpoint |
| `https://grafana.internal/d/worker-overview` | Worker job queue, failure rate, lag |
| `https://grafana.internal/d/infra` | CPU, memory, disk, goroutine count |
| `https://grafana.internal/explore?query=level%3Derror` | Live error log explorer |
| `https://grafana.internal/alerts` | Active alert list |

---

## 7. Mitigation Steps

### 7.1 Bad deployment — rollback

```bash
# Check current image
kubectl get deployment stellabill-backend -o jsonpath='{.spec.template.spec.containers[0].image}'

# Rollback to previous version
kubectl rollout undo deployment/stellabill-backend
kubectl rollout status deployment/stellabill-backend

# Confirm error rate is falling (give it 3 minutes)
```

### 7.2 Memory pressure / OOM

```bash
# Restart to free memory (will cause brief downtime — coordinate with load balancer)
kubectl rollout restart deployment/stellabill-backend
kubectl rollout status deployment/stellabill-backend

# Scale out horizontally if load is the cause
kubectl scale deployment stellabill-backend --replicas=<current+2>
```

### 7.3 Worker stuck or repeatedly failing

```bash
# Restart worker only (does not affect API pods)
kubectl rollout restart deployment/stellabill-worker
kubectl rollout status deployment/stellabill-worker

# Monitor worker recovery
journalctl -u stellabill-worker -f --no-pager | grep -E "error|started|completed"
```

### 7.4 Circuit breaker tripping on external dependency

```bash
# If billing service is the culprit, check its health
curl -sf https://billing-service.internal/health | jq .

# If external service is down, enable graceful degradation mode (if supported)
kubectl set env deployment/stellabill-backend BILLING_FALLBACK_MODE=true
# Remember to revert when the external service recovers
```

### 7.5 Rate limiting mis-configured (false 429s counted as errors)

```bash
# Check current rate limit config
kubectl exec -it deploy/stellabill-backend -- sh -c 'echo "RPS=$RATE_LIMIT_RPS BURST=$RATE_LIMIT_BURST"'

# Default: 10 RPS, 20 burst. If legitimate traffic is being rate-limited:
kubectl set env deployment/stellabill-backend RATE_LIMIT_RPS=50 RATE_LIMIT_BURST=100
# Calibrate carefully — too high enables abuse
```

---

## 8. Verification & Recovery

After applying a fix:

```bash
# 1. Health check
curl -sf https://api.stellabill.internal/api/health | jq .

# 2. Test all major endpoint groups
curl -sf https://api.stellabill.internal/api/plans \
     -H "Authorization: Bearer $TEST_TOKEN" -H "X-Tenant-ID: test-tenant" | jq .

curl -sf https://api.stellabill.internal/api/subscriptions \
     -H "Authorization: Bearer $TEST_TOKEN" -H "X-Tenant-ID: test-tenant" | jq .

# 3. Watch error rate drop (should approach 0 within 5 min of fix)
journalctl -u stellabill-backend --since "5 minutes ago" --no-pager -o json \
  | jq -r 'select(.status >= 500)' | wc -l
```

Declare recovery when:
- 5xx rate is below **0.5 %** for **10 consecutive minutes**
- Zero panics in the last 5 minutes
- `/api/health` returns `"status": "ok"` with all subsystems healthy
- Worker job failure rate is at or below pre-incident baseline

---

## 9. Escalation

| Condition | Escalate to |
|-----------|-------------|
| Rollback does not resolve errors | Backend team lead |
| Panics contain signs of data corruption | Backend lead + Data team |
| OOM persists after restart and scale-out | Infrastructure team |
| External billing service is down | Billing team / vendor support |
| > 30 min at Critical severity with no fix | Engineering manager |
| Security-sensitive data visible in error responses | Security team (immediately) |

---

## 10. Post-Incident Checklist

- [ ] Root cause documented in incident tracker
- [ ] Deployment pipeline reviewed — is there a canary/progressive rollout?
- [ ] Alert thresholds calibrated against measured baseline (§2)
- [ ] Panic frequency metric added to weekly engineering review if > 0
- [ ] Circuit breakers implemented for any external dependencies that caused failures
- [ ] Test coverage expanded to cover the error path that caused the incident
- [ ] Confirm no sensitive data (PII, secrets) appeared in any error logs during incident
- [ ] `docs/panic-recovery.md` updated if new panic recovery patterns were discovered