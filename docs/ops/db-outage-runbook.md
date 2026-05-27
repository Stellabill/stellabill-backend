# Runbook: Database Outages

**Service:** Stellabill Backend (Go/Gin + PostgreSQL)
**Owner:** On-call engineer
**Last updated:** 2026-04-23
**Related docs:** [`docs/outbox-pattern.md`](../outbox-pattern.md), [`docs/migrations.md`](../migrations.md)

---

## 1. Overview

This runbook covers PostgreSQL connectivity loss, connection pool exhaustion, replica lag, and slow query incidents affecting the Stellabill backend. The service connects via `DATABASE_URL` (never logged). The outbox pattern is used for transactional event publishing — a DB outage also halts event delivery.

When healthy, the `/api/health` endpoint returns:
```json
{"status": "ok", "db": "up", "worker": "running"}
```

During a DB outage `"db"` becomes `"degraded"` or `"down"`.

---

## 2. Alert Thresholds

All thresholds use a **1-minute evaluation window** unless noted.

| Alert | Condition | Severity | Pager? | Response SLA |
|-------|-----------|----------|--------|--------------|
| `db_connection_warning` | Connection errors > **5** per minute | ⚠️ Warning | No | 30 min |
| `db_connection_critical` | Connection errors > **20** per minute | 🔴 Critical | Yes | 10 min |
| `db_pool_exhaustion` | Available connections < **10 %** of pool max | 🔴 Critical | Yes | 10 min |
| `db_query_slow_warning` | p99 query latency > **500 ms** (5 min window) | ⚠️ Warning | No | 30 min |
| `db_query_slow_critical` | p99 query latency > **2 000 ms** (5 min window) | 🔴 Critical | Yes | 15 min |
| `db_health_check_fail` | `/api/health` returns `"db": "down"` for > **2 min** | 🔴 Critical | Yes | 5 min |
| `db_replica_lag_warning` | Replication lag > **30 s** | ⚠️ Warning | No | 30 min |
| `db_replica_lag_critical` | Replication lag > **5 min** | 🔴 Critical | Yes | 10 min |
| `worker_job_failures` | Background worker job failures > **10** in 5 min | ⚠️ Warning | No | 30 min |

> **Pool max default:** Go `sql.DB` defaults to unlimited; confirm `DB_MAX_OPEN_CONNS` is set in your deployment config.

---

## 3. What to Check First (Triage Checklist)

Run through this list **in order**.

- [ ] **1. Is PostgreSQL process running?**  
  A down process is the most common cause — check it before anything else.

- [ ] **2. Can the API host reach PostgreSQL at all?**  
  Network partition vs. PostgreSQL crash are treated differently.

- [ ] **3. Are connections exhausted, or is PostgreSQL refusing connections?**  
  Pool exhaustion (connection count at max) vs. PostgreSQL `max_connections` limit hit vs. process down are three distinct failure modes.

- [ ] **4. Is the primary affected, or only a replica?**  
  Read-only replicas failing affects reads. Primary down halts all writes, outbox delivery, and worker jobs.

- [ ] **5. Did a migration run recently?**  
  Long-running DDL migrations lock tables and can look like a partial outage. Check migration history.

- [ ] **6. Is disk space a factor?**  
  PostgreSQL stops writing when the disk is full — check disk before restarting.

---

## 4. Log Queries

### 4.1 Count DB connection errors (last 30 min)

```bash
journalctl -u stellabill-backend --since "30 minutes ago" --no-pager -o json \
  | jq -r 'select(.level == "error") | select(.message | test("database|connection|sql|pgx|pool")) | .message' \
  | sort | uniq -c | sort -rn
```

### 4.2 Find slow query log entries

```bash
journalctl -u stellabill-backend --since "1 hour ago" --no-pager -o json \
  | jq -r 'select(.duration_ms != null) | select(.duration_ms > 500) | {time: .REALTIME_TIMESTAMP, query: .query_name, duration_ms: .duration_ms, trace: .trace_id}'
```

### 4.3 Worker job failures linked to DB

```bash
journalctl -u stellabill-worker --since "1 hour ago" --no-pager -o json \
  | jq -r 'select(.level == "error") | {time: .REALTIME_TIMESTAMP, job: .job_type, error: .error}'
```

### 4.4 Check PostgreSQL logs directly

```bash
# Adjust path for your PostgreSQL installation
sudo journalctl -u postgresql --since "1 hour ago" --no-pager | grep -E "ERROR|FATAL|PANIC|connection"
```

> **Security note:** `DATABASE_URL` (which contains credentials) is never written to logs. If you find it in any log entry, treat this as a security incident and rotate credentials immediately.

---

## 5. Diagnostic Commands

```bash
# 1. Is PostgreSQL running?
systemctl status postgresql

# 2. Can we connect? (use a non-privileged read-only user for this check)
psql "$DATABASE_URL" -c "SELECT 1;" 2>&1

# 3. How many connections are open?
psql "$DATABASE_URL" -c "SELECT count(*), state FROM pg_stat_activity GROUP BY state;"

# 4. What is the max_connections setting?
psql "$DATABASE_URL" -c "SHOW max_connections;"

# 5. Are any queries blocked (lock waits)?
psql "$DATABASE_URL" -c "
  SELECT pid, now() - pg_stat_activity.query_start AS duration, query, state
  FROM pg_stat_activity
  WHERE state != 'idle' AND query_start < now() - interval '30 seconds'
  ORDER BY duration DESC LIMIT 10;"

# 6. Replication lag (if replicas are configured)
psql "$DATABASE_URL" -c "
  SELECT client_addr, state, sent_lsn, write_lsn,
         (sent_lsn - write_lsn) AS lag_bytes
  FROM pg_stat_replication;"

# 7. Disk space
df -h /var/lib/postgresql
```

---

## 6. Dashboard Links

| Dashboard | Purpose |
|-----------|---------|
| `https://grafana.internal/d/db-overview` | Connection pool, query latency, error rate |
| `https://grafana.internal/d/pg-internals` | PostgreSQL connections, lock waits, replication lag |
| `https://grafana.internal/d/worker-overview` | Background worker job queue depth and failures |
| `https://grafana.internal/explore?query=error+database` | Live log explorer filtered to DB errors |

---

## 7. Mitigation Steps

### 7.1 PostgreSQL process is down

```bash
# Attempt restart
sudo systemctl start postgresql
sudo systemctl status postgresql

# Monitor startup — watch for "database system is ready to accept connections"
sudo journalctl -u postgresql -f --no-pager | head -50
```

### 7.2 Connection pool exhausted

```bash
# Restart the API to clear stale connections from the pool
kubectl rollout restart deployment/stellabill-backend
kubectl rollout status deployment/stellabill-backend

# Optionally terminate idle connections from the PostgreSQL side
psql "$DATABASE_URL" -c "
  SELECT pg_terminate_backend(pid)
  FROM pg_stat_activity
  WHERE state = 'idle'
    AND query_start < now() - interval '5 minutes'
    AND application_name = 'stellabill-backend';"
```

### 7.3 Activate read-only mode

Use when the primary is unavailable but reads must continue (e.g., listing plans/subscriptions):

```bash
kubectl set env deployment/stellabill-backend DB_READONLY=true
# This disables write endpoints and background workers
# Verify the flag took effect:
curl -sf https://api.stellabill.internal/api/health | jq .
```

**Revert read-only mode** once the primary recovers:
```bash
kubectl set env deployment/stellabill-backend DB_READONLY-
kubectl rollout status deployment/stellabill-backend
```

### 7.4 Long-running migration lock

```bash
# Find the blocking migration query
psql "$DATABASE_URL" -c "
  SELECT pid, query, state, wait_event_type, wait_event
  FROM pg_stat_activity
  WHERE wait_event_type = 'Lock';"

# If safe to terminate (confirm with DBA first):
psql "$DATABASE_URL" -c "SELECT pg_terminate_backend(<pid>);"
```

### 7.5 Disk full

```bash
# Free space by cleaning WAL archive if safe
sudo find /var/lib/postgresql/*/pg_wal -name "*.partial" -mtime +1 -delete

# Alert DBA immediately — do not restart PostgreSQL with a full disk
```

---

## 8. Verification & Recovery

After applying a fix, confirm full recovery:

```bash
# 1. Health check (all three fields should be "up"/"running")
curl -sf https://api.stellabill.internal/api/health | jq .

# 2. Write test (create and immediately cancel a test subscription — or use a staging tenant)
curl -sf -X POST https://api.stellabill.internal/api/subscriptions \
     -H "Authorization: Bearer $TEST_TOKEN" \
     -H "X-Tenant-ID: test-tenant" \
     -H "Content-Type: application/json" \
     -d '{"plan_id":"test-plan"}' | jq .

# 3. Confirm connection pool is healthy (error count should be 0 or near 0)
journalctl -u stellabill-backend --since "5 minutes ago" --no-pager -o json \
  | jq -r 'select(.message | test("database|connection")) | select(.level == "error")' | wc -l
```

Declare recovery when:
- `/api/health` returns `"db": "up"` for **5 consecutive minutes**
- Connection error rate is below **1 per minute**
- Worker job failure rate returns to baseline

---

## 9. Escalation

| Condition | Escalate to |
|-----------|-------------|
| PostgreSQL won't start after restart | DBA / Infrastructure team |
| Data loss suspected | DBA + Engineering manager (immediately) |
| Replication lag > 30 min | DBA |
| Disk full with no quick path to free space | Infrastructure team |
| > 30 min at Critical severity with no fix | Engineering manager |

---

## 10. Post-Incident Checklist

- [ ] Root cause documented in incident tracker
- [ ] `DB_MAX_OPEN_CONNS` and `DB_MAX_IDLE_CONNS` tuning reviewed
- [ ] Migration process reviewed — are long-running migrations run with lock timeouts?
- [ ] Replica failover procedure tested (if applicable)
- [ ] Confirm no credentials were written to logs during investigation
- [ ] Alert thresholds calibrated against measured p99 latency and connection baseline
- [ ] Outbox event backlog cleared after recovery (no duplicate events delivered)