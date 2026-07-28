-- Add tenant_id and partition columns for shard-aware outbox dispatching.
-- Existing rows get partition = 0 (default), which maps to shard 0.

ALTER TABLE outbox_events
    ADD COLUMN IF NOT EXISTS tenant_id VARCHAR(255);

ALTER TABLE outbox_events
    ADD COLUMN IF NOT EXISTS partition INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_outbox_events_partition
    ON outbox_events (partition);

CREATE INDEX IF NOT EXISTS idx_outbox_events_tenant_id
    ON outbox_events (tenant_id);

-- Backfill partition for rows that already have a tenant_id.
-- Uses hashtext() which is available in all supported PostgreSQL versions.
UPDATE outbox_events
SET    partition = abs(hashtext(tenant_id)) % 8
WHERE  tenant_id IS NOT NULL
  AND  partition = 0;
