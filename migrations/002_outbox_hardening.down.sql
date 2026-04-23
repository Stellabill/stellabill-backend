-- 002_outbox_hardening.down.sql
-- Reverts changes introduced by 002_outbox_hardening.up.sql.

DROP INDEX IF EXISTS idx_outbox_events_status_updated_at;
DROP INDEX IF EXISTS idx_outbox_events_dedupe_key;

ALTER TABLE outbox_events
    DROP COLUMN IF EXISTS dedupe_key;
