# Outbox Operations Runbook

This runbook covers the four most common outbox failure modes in Stellabill. Each section includes symptoms, diagnostic SQL, recovery steps, and prevention measures.

---

## 1. Events Stuck in `processing` Status

### Symptoms
- `outbox_events` rows with `status = 'processing'` and `updated_at` older than the configured `ProcessingTimeout` (default 30 s).
- Downstream consumers stop receiving new events.
- Dispatcher logs show no "Successfully published" lines for affected event types.

### Diagnostic SQL

```sql
-- Count events stuck in processing
SELECT COUNT(*) AS stuck_count
FROM outbox_events
WHERE status = 'processing'
  AND updated_at < NOW() - INTERVAL '30 seconds';

-- List stuck events (most recent first)
SELECT id, event_type, retry_count, updated_at, error_message
FROM outbox_events
WHERE status = 'processing'
  AND updated_at < NOW() - INTERVAL '30 seconds'
ORDER BY updated_at ASC
LIMIT 50;
```

```bash
# Run via psql
psql "$DATABASE_URL" -c "
  SELECT id, event_type, retry_count, updated_at
  FROM outbox_events
  WHERE status = 'processing' AND updated_at < NOW() - INTERVAL '30 seconds'
  ORDER BY updated_at ASC LIMIT 20;
"
```

### Recovery Steps

**Automatic recovery**: The Dispatcher calls `RecoverStuckEvents` on every poll cycle. Events stuck longer than `ProcessingTimeout` are automatically reset to `pending` with an incremented `retry_count`. No manual intervention is needed if the Dispatcher is running.

**Manual recovery** (if Dispatcher is down):

```sql
-- Reset all stuck processing events to pending
UPDATE outbox_events
SET status = 'pending',
    retry_count = retry_count + 1,
    updated_at  = NOW()
WHERE status = 'processing'
  AND updated_at < NOW() - INTERVAL '30 seconds';
```

**Via API** (if Dispatcher is running but a specific event is stuck):

```go
// Using the Service
err := service.RequeueEvent(eventID)
```

### Prevention
- Keep `ProcessingTimeout` shorter than the publisher's HTTP timeout.
- Ensure the Dispatcher process has a liveness probe and is restarted on crash.
- Monitor `outbox_events WHERE status = 'processing' AND updated_at < NOW() - INTERVAL '1 minute'` — alert if count > 0.

---

## 2. Dead-Lettered Events (`status = 'failed'`, `retry_count >= max_retries`)

### Symptoms
- Events with `status = 'failed'` and `retry_count >= max_retries` accumulate.
- Downstream consumers are missing events.
- Dispatcher logs show "Event X failed after N retries".

### Diagnostic SQL

```sql
-- Count dead-lettered events
SELECT COUNT(*) AS dead_letter_count
FROM outbox_events
WHERE status = 'failed'
  AND retry_count >= max_retries;

-- List dead-lettered events with error details
SELECT id, event_type, retry_count, max_retries, error_message, updated_at
FROM outbox_events
WHERE status = 'failed'
  AND retry_count >= max_retries
ORDER BY updated_at ASC
LIMIT 50;
```

```bash
psql "$DATABASE_URL" -c "
  SELECT id, event_type, retry_count, error_message
  FROM outbox_events
  WHERE status = 'failed' AND retry_count >= max_retries
  ORDER BY updated_at ASC LIMIT 20;
"
```

### Recovery Steps

**Via API** (recommended — resets retry_count to 0 and status to pending):

```go
// List dead-lettered events
events, err := service.ListDeadLetterEvents(100)

// Requeue a specific event
err = service.RequeueEvent(eventID)
```

**Via SQL** (bulk requeue):

```sql
-- Requeue all dead-lettered events
UPDATE outbox_events
SET status      = 'pending',
    retry_count = 0,
    next_retry_at = NULL,
    updated_at  = NOW()
WHERE status = 'failed'
  AND retry_count >= max_retries;
```

```bash
psql "$DATABASE_URL" -c "
  UPDATE outbox_events
  SET status = 'pending', retry_count = 0, next_retry_at = NULL, updated_at = NOW()
  WHERE status = 'failed' AND retry_count >= max_retries;
"
```

**Before requeuing**, investigate the `error_message` to fix the root cause (e.g., downstream endpoint unreachable, malformed payload).

### Prevention
- Set `max_retries` high enough for transient failures (default 3 is conservative; consider 5–10 for production).
- Alert when `dead_letter_count > 0`.
- Ensure the downstream HTTP endpoint is monitored independently.

---

## 3. High Queue Depth (Pending Events Accumulating)

### Symptoms
- Large number of events with `status = 'pending'` or `status = 'failed'` with `next_retry_at <= NOW()`.
- Processing latency increases.
- Dispatcher logs show "Processing N pending events" repeatedly with N at batch limit.

### Diagnostic SQL

```sql
-- Queue depth by status
SELECT status, COUNT(*) AS count
FROM outbox_events
GROUP BY status
ORDER BY count DESC;

-- Oldest pending event (processing lag)
SELECT MIN(occurred_at) AS oldest_pending,
       NOW() - MIN(occurred_at) AS lag
FROM outbox_events
WHERE status = 'pending';

-- Events ready for retry
SELECT COUNT(*) AS retry_ready
FROM outbox_events
WHERE status = 'failed'
  AND next_retry_at <= NOW()
  AND retry_count < max_retries;
```

```bash
psql "$DATABASE_URL" -c "
  SELECT status, COUNT(*) FROM outbox_events GROUP BY status;
"
```

### Recovery Steps

1. **Increase batch size**: Set `OUTBOX_BATCH_SIZE` to a higher value (e.g., 50–100) and restart the Dispatcher.
2. **Decrease poll interval**: Set `OUTBOX_POLL_INTERVAL` to `1s` or `2s` temporarily.
3. **Scale horizontally**: Run multiple Dispatcher instances — they coordinate via `MarkAsProcessing` optimistic locking.
4. **Check publisher throughput**: If the downstream endpoint is slow, the bottleneck is there. Consider rate-limiting or batching at the publisher level.

### Prevention
- Index `(status, occurred_at)` and `(status, next_retry_at)` — already created by migration `002_outbox_hardening.up.sql`.
- Set up a queue-depth alert: `pending + retry_ready > 1000`.
- Run periodic cleanup of completed events (`OUTBOX_CLEANUP_INTERVAL`, `OUTBOX_COMPLETED_EVENT_TTL`).

---

## 4. Database Connectivity Loss

### Symptoms
- Dispatcher logs show "Failed to get pending events: ..." or "Failed to recover stuck events: ..." repeatedly.
- No events are being processed.
- Health endpoint returns `database_health: unhealthy`.

### Diagnostic SQL

```bash
# Test connectivity
psql "$DATABASE_URL" -c "SELECT 1;"

# Check active connections
psql "$DATABASE_URL" -c "
  SELECT count(*) AS active_connections
  FROM pg_stat_activity
  WHERE datname = current_database();
"

# Check for lock contention on outbox_events
psql "$DATABASE_URL" -c "
  SELECT pid, state, wait_event_type, wait_event, query
  FROM pg_stat_activity
  WHERE query ILIKE '%outbox_events%'
    AND state != 'idle';
"
```

### Recovery Steps

1. **Dispatcher is self-healing**: Once DB connectivity is restored, the Dispatcher automatically resumes on the next poll cycle. No manual intervention is needed.
2. **Verify connection pool**: Check `DATABASE_URL` and connection pool settings (`DB_MAX_OPEN_CONNS`, `DB_MAX_IDLE_CONNS`).
3. **Check PostgreSQL logs** for OOM, disk full, or max_connections exceeded.
4. **Restart Dispatcher** if it has entered a crash loop (check process health).

### Prevention
- Configure connection pool with appropriate `max_open_conns` (default: 25) and `conn_max_lifetime` (default: 5 min).
- Use a connection pooler (PgBouncer) in front of PostgreSQL for high-concurrency deployments.
- Set up a DB connectivity alert separate from the outbox queue-depth alert.
- The Dispatcher logs errors and continues — it will not crash on DB errors. Verify this behavior is preserved in all deployments.

---

## Recovery Tool Reference

| Tool | Usage |
|---|---|
| `service.ListDeadLetterEvents(limit)` | List events with `status=failed` and `retry_count >= max_retries` |
| `service.RequeueEvent(id)` | Reset a dead-lettered event to `pending` with `retry_count=0` |
| `RecoverStuckEvents(olderThan)` | Called automatically by Dispatcher each poll cycle; resets `processing` → `pending` |
| `GET /api/outbox/stats` | Returns queue depth, dispatcher status, DB health |

## Key Configuration Variables

| Variable | Default | Description |
|---|---|---|
| `OUTBOX_POLL_INTERVAL` | `5s` | How often the Dispatcher polls for pending events |
| `OUTBOX_BATCH_SIZE` | `10` | Events processed per poll cycle |
| `OUTBOX_MAX_RETRIES` | `3` | Max retry attempts before dead-lettering |
| `OUTBOX_PROCESSING_TIMEOUT` | `30s` | Max time an event may stay in `processing` before recovery |
| `OUTBOX_RETRY_BACKOFF_FACTOR` | `2.0` | Exponential backoff multiplier |
| `OUTBOX_COMPLETED_EVENT_TTL` | `24h` | How long completed events are retained |
