-- Revert to the base-column dead-letter view created by 0007.
CREATE OR REPLACE VIEW dead_letter_events AS
SELECT id, event_type, event_data, aggregate_id, aggregate_type,
       occurred_at, status, retry_count, max_retries, next_retry_at,
       error_message, created_at, updated_at, version
FROM outbox_events
WHERE status = 'failed'
ORDER BY occurred_at DESC;