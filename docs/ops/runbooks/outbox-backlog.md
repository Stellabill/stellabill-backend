<!-- markdownlint-disable MD013 -->
# Runbook: Outbox Backlog Remediation

**Service:** Stellabill Backend (`internal/outbox`, `internal/worker`)  
**Owner:** On-call engineer  
**Last updated:** 2026-07-27  
**Related docs:** [`../../outbox-pattern.md`](../../outbox-pattern.md), [`../README.md`](../README.md), [`../elevated-errors-runbook.md`](../elevated-errors-runbook.md), [`../../runbooks/chaos-outbox.md`](../../runbooks/chaos-outbox.md), [`../keda-worker-autoscale.md`](../keda-worker-autoscale.md)

---

## 1. Overview

This runbook provides step-by-step instructions for on-call engineers responding to elevated outbox backlog depth (`outbox_backlog_depth`) or event publishing stalls in the Stellabill backend.

The outbox system decouples transactional state changes from asynchronous event delivery (webhooks, notifications, downstream synchronizations). Events are written atomically into the `outbox_events` table during business operations with an initial status of `pending`. The outbox worker and dispatcher poll pending events, publish them to configured destinations (`http_publisher`, `jwe_publisher`, `slack_publisher`), record publisher positions in `outbox_publisher_progress`, and record delivery logs in `outbox_attempts`.

When `outbox_backlog_depth` increases, events accumulate in `pending` state faster than publishers process them. This signals publisher stalls, downstream receiver outages, database lock contention, or network connectivity failures.

---

## 2. Alert Thresholds & Annotations

The following alerting rules monitor outbox processing health and link directly to this runbook:

| Alert Name | Condition | Severity | Pager? | Response SLA | Runbook Annotation Link |
| --- | --- | --- | --- | --- | --- |
| `outbox_backlog_depth_warning` | `outbox_backlog_depth` > **500** for 5 min | Warning | No | 30 min | [`docs/ops/runbooks/outbox-backlog.md`](./outbox-backlog.md) |
| `outbox_backlog_depth_critical` | `outbox_backlog_depth` > **2,500** for 5 min | Critical | Yes | 15 min | [`docs/ops/runbooks/outbox-backlog.md`](./outbox-backlog.md) |
| `outbox_publisher_lag_critical` | `outbox_publisher_lag_seconds` > **300s** for 5 min | Critical | Yes | 15 min | [`docs/ops/runbooks/outbox-backlog.md`](./outbox-backlog.md) |
| `outbox_deadletter_inflow_spike` | `dead_letter_events` count >= **5** in 1 min | Critical | Yes | 15 min | [`docs/ops/runbooks/outbox-backlog.md`](./outbox-backlog.md) |
| `outbox_dispatcher_stalled` | Dispatcher active = false or 0 throughput with backlog > 0 | Critical | Yes | 10 min | [`docs/ops/runbooks/outbox-backlog.md`](./outbox-backlog.md) |

---

## 3. Grafana Dashboard Panels & Visualizations

During an incident, navigate to the Grafana dashboard **Stellabill Outbox Operations** (`https://grafana.internal.stellabill.com/d/outbox-ops`).

### 3.1 Outbox Backlog Depth Panel

```text
+-----------------------------------------------------------------------------------+
| Panel: Outbox Backlog Depth (outbox_backlog_depth)                                |
| PromQL: sum(outbox_events_status_count{status="pending"}) by (event_type)        |
|                                                                                   |
|  Events                                                                           |
|   3000 +---------------------------------------------------------/\-------------  | Critical: 2500
|   2000 |                                                        /  \             |
|   1000 |                                  /--------------------+    \            |
|    500 +---------------------------------/---------------------------------------  | Warning: 500
|      0 +--------------------------------/----------------------------------------  |
|        00:00        00:15        00:30        00:45        01:00        01:15     |
|        Legend: [-- subscription.created] [-- invoice.paid] [-- webhook.delivery] |
+-----------------------------------------------------------------------------------+
```

### 3.2 Publisher Lag & Throughput Panel

```text
+-----------------------------------------------------------------------------------+
| Panel: Publisher Lag & Event Processing Rate                                      |
| PromQL: outbox_publisher_lag_seconds / rate(outbox_events_published_total[5m])   |
|                                                                                   |
|   Lag(s)                                                                 Rate(ev/s)|
|    600s +-------------------------------------------------------+-------- 50 ev/s |
|    300s +-----------------------------------/\------------------+-------- 25 ev/s |
|      0s +----------------------------------/--\-----------------+--------  0 ev/s |
|        Legend: [-- http_publisher lag] [-- slack_publisher lag] [-- publish rate] |
+-----------------------------------------------------------------------------------+
```

### 3.3 Dead-Letter Inflow & Delivery Attempt Failures Panel

```text
+-----------------------------------------------------------------------------------+
| Panel: Dead-Letter Events Inflow & Failure Code Breakdown                         |
| PromQL: rate(outbox_deadletter_inflow_total[5m]) by (response_code)               |
|                                                                                   |
|  Failures/s                                                                       |
|     10 +----------------------------------/\------------------------------------- |
|      5 +---------------------------------/  \------------------------------------ |
|      0 +--------------------------------/----\----------------------------------- |
|        Legend: [504 Gateway Timeout] [401 Unauthorized] [Connection Refused]     |
+-----------------------------------------------------------------------------------+
```

---

## 4. Triage Checklist (What to Check First)

Execute the following checklist in sequence when alerted:

- [ ] **1. Is the Outbox Worker process running?**  
  Check `/api/health` for `"outbox": {"dispatcher_running": true}` and verify systemd/container process status. If stopped, jump to [Section 8: Publisher Restart Procedures](#8-publisher-restart--recovery-procedures).

- [ ] **2. Is backlog specific to one event type or tenant?**  
  Execute Triage Query 5.1. If only `invoice.paid` is accumulating, the issue is isolated to a specific handler/publisher route rather than general DB failure.

- [ ] **3. Is a single publisher lagging behind?**  
  Execute Triage Query 5.3 to inspect `outbox_publisher_progress`. Determine if `http_publisher`, `jwe_publisher`, or `slack_publisher` has stopped updating its `last_event_id`.

- [ ] **4. Are downstream endpoints returning HTTP errors?**  
  Execute Triage Query 5.2 and Query 5.4. Check if attempts are failing due to target HTTP 504, 429 rate limits, or bad TLS handshake.

- [ ] **5. Is the Dead-Letter Queue (DLQ) accumulating failed events?**  
  Execute Triage Query 5.5. Events exceeding `max_retries` transition to status `failed` and move to `dead_letter_events`.

---

## 5. Copy-Paste-Safe SQL Triage Queries

All triage queries are safe read-only (`SELECT`) statements. Run via production read-replica or `psql`.

### 5.1 Backlog depth count by status and event type

```sql
SELECT status, event_type, COUNT(*) AS event_count, MIN(created_at) AS oldest_created_at
FROM outbox_events
WHERE status IN ('pending', 'processing', 'failed')
GROUP BY status, event_type
ORDER BY event_count DESC;
```

### 5.2 Failure error breakdown from recent delivery attempts

```sql
SELECT response_code, error_message, COUNT(*) AS attempt_count
FROM outbox_attempts
WHERE attempted_at >= NOW() - INTERVAL '1 hour'
GROUP BY response_code, error_message
ORDER BY attempt_count DESC
LIMIT 10;
```

### 5.3 Publisher progress and high-water mark lag

```sql
SELECT publisher, last_event_id, updated_at, NOW() - updated_at AS idle_duration
FROM outbox_publisher_progress
ORDER BY updated_at ASC;
```

### 5.4 Delivery attempt history for recent failed/pending events

```sql
SELECT a.event_id, e.event_type, a.tenant_id, a.attempt_number,
       a.response_code, a.error_message, a.attempted_at
FROM outbox_attempts a
JOIN outbox_events e ON a.event_id = e.id
WHERE e.status IN ('failed', 'pending')
ORDER BY a.attempted_at DESC
LIMIT 20;
```

### 5.5 Dead-Letter Queue (DLQ) event inspection

```sql
SELECT id, event_type, aggregate_type, aggregate_id, retry_count,
       max_retries, error_message, created_at, updated_at
FROM dead_letter_events
ORDER BY updated_at DESC
LIMIT 25;
```

### 5.6 Oldest pending event age check

```sql
SELECT id, event_type, status, retry_count, next_retry_at,
       NOW() - created_at AS backlog_age
FROM outbox_events
WHERE status = 'pending'
ORDER BY created_at ASC
LIMIT 5;
```

---

## 6. Safe DLQ Replay Procedures

When failed events accumulate in `dead_letter_events` due to resolved transient network partitions or downstream outages, replay them using the safe procedures below.

### 6.1 Pre-Replay Safeguards

1. Confirm downstream endpoint is healthy and capable of accepting replayed webhooks.
2. Confirm event handlers are idempotent.
3. Limit replay batches to <= 100 events per execution to avoid throttling downstream receivers.

### 6.2 Targeted Replay by Event ID List

```sql
BEGIN;

UPDATE outbox_events
SET status = 'pending',
    retry_count = 0,
    next_retry_at = NOW(),
    error_message = NULL,
    updated_at = NOW()
WHERE id IN (
    '00000000-0000-0000-0000-000000000001'::uuid,
    '00000000-0000-0000-0000-000000000002'::uuid
)
AND status = 'failed';

COMMIT;
```

### 6.3 Batch Replay by Event Type and Time Window

```sql
BEGIN;

WITH target_events AS (
    SELECT id
    FROM outbox_events
    WHERE status = 'failed'
      AND event_type = 'invoice.paid'
      AND updated_at >= NOW() - INTERVAL '2 hours'
    LIMIT 50
    FOR UPDATE SKIP LOCKED
)
UPDATE outbox_events
SET status = 'pending',
    retry_count = 0,
    next_retry_at = NOW(),
    error_message = NULL,
    updated_at = NOW()
FROM target_events
WHERE outbox_events.id = target_events.id;

COMMIT;
```

---

## 7. Rollback Procedure (Misapplied Replay Remediation)

If a DLQ replay was misapplied (e.g., target receiver remains unresponsive or duplicate flood occurs), immediately execute one of the rollback queries below to revert replayed events to `failed` state and halt processing.

### 7.1 Bulk Replay Rollback Query

```sql
BEGIN;

UPDATE outbox_events
SET status = 'failed',
    error_message = 'Replay rolled back by on-call engineer: downstream target unready',
    updated_at = NOW()
WHERE status = 'pending'
  AND updated_at >= NOW() - INTERVAL '15 minutes'
  AND retry_count = 0;

COMMIT;
```

### 7.2 ID-Specific Replay Rollback Query

```sql
BEGIN;

UPDATE outbox_events
SET status = 'failed',
    error_message = 'Replay rolled back manually',
    updated_at = NOW()
WHERE id IN (
    '00000000-0000-0000-0000-000000000001'::uuid,
    '00000000-0000-0000-0000-000000000002'::uuid
)
AND status = 'pending';

COMMIT;
```

---

## 8. Publisher Restart & Recovery Procedures

### 8.1 Restarting the Worker Service

```bash
# Check worker service health
systemctl status stellabill-worker

# Restart outbox worker process
sudo systemctl restart stellabill-worker

# Tail outbox worker logs
journalctl -u stellabill-worker -n 50 --no-pager | grep "outbox"
```

### 8.2 Resetting Publisher Progress High-Water Mark

If a publisher is permanently stuck on an unprocessable event ID:

```sql
-- Check current progress
SELECT * FROM outbox_publisher_progress WHERE publisher = 'http_publisher';

-- Advance progress to safe event ID (Requires Team Lead Approval)
BEGIN;

UPDATE outbox_publisher_progress
SET last_event_id = '00000000-0000-0000-0000-000000000000'::uuid,
    updated_at = NOW()
WHERE publisher = 'http_publisher';

COMMIT;
```

---

## 9. Escalation Criteria

| Trigger | Escalation Target | SLA | Required Action |
| --- | --- | --- | --- |
| Backlog > **10,000** pending events | Backend Lead | 15 min | Scale out worker concurrency (confirm KEDA HPA desired is at `maxReplicaCount`; see [`../keda-worker-autoscale.md`](../keda-worker-autoscale.md)) |
| DB lock contention on `outbox_events` | DBA / Infra | 15 min | Vacuum / reindex outbox tables |
| External endpoint outage > **2 hours** | Product Ops | 30 min | Notify affected tenants |
| Data payload corruption detected | Security Lead | Immediate | Pause dispatcher and quarantine rows |

---

## 10. Security & Compliance Requirements

- **Sensitive Data Redaction**: Do not log raw authorization header tokens or unscrubbed payload secrets.
- **Audit Logging**: Production table updates must be executed through audited db management tools.
