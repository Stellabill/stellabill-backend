ALTER TABLE outbox_events ADD COLUMN IF NOT EXISTS deduplication_id VARCHAR(255);

CREATE UNIQUE INDEX IF NOT EXISTS idx_outbox_events_deduplication_id
    ON outbox_events(deduplication_id)
    WHERE deduplication_id IS NOT NULL;
