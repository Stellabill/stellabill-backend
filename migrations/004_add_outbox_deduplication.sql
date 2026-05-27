-- Add deduplication_id to outbox_events to support idempotency
ALTER TABLE outbox_events ADD COLUMN deduplication_id VARCHAR(255);

-- Create a unique index for deduplication_id to prevent duplicate events
-- We use a partial index to allow NULL values (though ideally all mutation events will have one)
CREATE UNIQUE INDEX idx_outbox_events_deduplication_id ON outbox_events(deduplication_id) WHERE deduplication_id IS NOT NULL;
