-- Persist per-publisher outbox delivery progress to avoid replaying events
-- that were already acknowledged before a dispatcher restart.
CREATE TABLE IF NOT EXISTS outbox_publisher_progress (
    publisher VARCHAR(255) PRIMARY KEY,
    last_event_id UUID,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

ALTER TABLE outbox_publisher_progress
    ADD COLUMN IF NOT EXISTS last_event_id UUID;

DELETE FROM outbox_publisher_progress
    WHERE last_event_id IS NULL;

ALTER TABLE outbox_publisher_progress
    ALTER COLUMN last_event_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_outbox_events_id_status
    ON outbox_events(id, status);
