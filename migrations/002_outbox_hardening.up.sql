-- 002_outbox_hardening.up.sql
-- Adds dedupe_key column and supporting indexes for outbox hardening.

ALTER TABLE outbox_events
    ADD COLUMN IF NOT EXISTS dedupe_key VARCHAR(255);

-- Partial unique index: enforces uniqueness only for non-null dedupe_key values,
-- preserving backward compatibility with legacy events that have no key.
CREATE UNIQUE INDEX IF NOT EXISTS idx_outbox_events_dedupe_key
    ON outbox_events(dedupe_key)
    WHERE dedupe_key IS NOT NULL;

-- Composite index to support RecoverStuckEvents queries efficiently.
CREATE INDEX IF NOT EXISTS idx_outbox_events_status_updated_at
    ON outbox_events(status, updated_at);
