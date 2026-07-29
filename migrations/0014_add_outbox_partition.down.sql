DROP INDEX IF EXISTS idx_outbox_events_tenant_id;
DROP INDEX IF EXISTS idx_outbox_events_partition;

ALTER TABLE outbox_events DROP COLUMN IF EXISTS partition;
ALTER TABLE outbox_events DROP COLUMN IF EXISTS tenant_id;
