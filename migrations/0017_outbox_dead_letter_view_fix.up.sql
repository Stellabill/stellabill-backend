-- Extend the outbox dead-letter view so it exposes the full event projection
-- expected by the Repository scan (tenant_id, deduplication_id, partition).
-- The original 0007 view exposed only the base columns, which caused the
-- ListDeadLetteredEvents scan to reference columns that did not exist on the view.
CREATE OR REPLACE VIEW dead_letter_events AS
SELECT id, tenant_id, event_type, event_data, aggregate_id, aggregate_type,
       occurred_at, status, retry_count, max_retries, next_retry_at,
       error_message, created_at, updated_at, version, deduplication_id,
       partition
FROM outbox_events
WHERE status = 'failed'
ORDER BY occurred_at DESC;