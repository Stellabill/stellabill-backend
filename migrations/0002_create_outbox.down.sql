DROP TRIGGER IF EXISTS trigger_update_outbox_updated_at ON outbox_events;
DROP FUNCTION IF EXISTS update_outbox_updated_at();

DROP INDEX IF EXISTS idx_outbox_events_occurred_at;
DROP INDEX IF EXISTS idx_outbox_events_aggregate;
DROP INDEX IF EXISTS idx_outbox_events_next_retry;
DROP INDEX IF EXISTS idx_outbox_events_status;

DROP TABLE IF EXISTS outbox_events;
