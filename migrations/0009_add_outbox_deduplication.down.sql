DROP INDEX IF EXISTS idx_outbox_events_deduplication_id;
ALTER TABLE outbox_events DROP COLUMN IF EXISTS deduplication_id;
