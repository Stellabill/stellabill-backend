DROP POLICY IF EXISTS tenant_isolation_outbox_events ON outbox_events;
DROP POLICY IF EXISTS tenant_isolation_statements_partitioned ON statements_partitioned;
DROP POLICY IF EXISTS tenant_isolation_statements ON statements;
DROP POLICY IF EXISTS tenant_isolation_subscriptions ON subscriptions;
DROP POLICY IF EXISTS tenant_isolation_plans ON plans;

ALTER TABLE outbox_events DISABLE ROW LEVEL SECURITY;
ALTER TABLE statements_partitioned DISABLE ROW LEVEL SECURITY;
ALTER TABLE statements DISABLE ROW LEVEL SECURITY;
ALTER TABLE subscriptions DISABLE ROW LEVEL SECURITY;
ALTER TABLE plans DISABLE ROW LEVEL SECURITY;

DROP INDEX IF EXISTS idx_outbox_events_tenant_id;
DROP INDEX IF EXISTS idx_statements_part_tenant_id;
DROP INDEX IF EXISTS idx_statements_tenant_id;
DROP INDEX IF EXISTS idx_subscriptions_tenant_id;
DROP INDEX IF EXISTS idx_plans_tenant_id;

ALTER TABLE outbox_events DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE statements_partitioned DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE statements DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE plans DROP COLUMN IF EXISTS tenant_id;
