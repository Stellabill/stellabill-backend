ALTER TABLE plans ADD COLUMN IF NOT EXISTS tenant_id TEXT;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS tenant_id TEXT;
ALTER TABLE statements ADD COLUMN IF NOT EXISTS tenant_id TEXT;
ALTER TABLE statements_partitioned ADD COLUMN IF NOT EXISTS tenant_id TEXT;
ALTER TABLE outbox_events ADD COLUMN IF NOT EXISTS tenant_id TEXT;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'plans' AND column_name = 'tenant_id'
    ) THEN
        UPDATE plans SET tenant_id = COALESCE(
            current_setting('app.tenant_id', true),
            'bootstrap-tenant'
        ) WHERE tenant_id IS NULL;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'subscriptions' AND column_name = 'tenant_id'
    ) THEN
        UPDATE subscriptions SET tenant_id = COALESCE(
            current_setting('app.tenant_id', true),
            'bootstrap-tenant'
        ) WHERE tenant_id IS NULL;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'statements' AND column_name = 'tenant_id'
    ) THEN
        UPDATE statements SET tenant_id = COALESCE(
            current_setting('app.tenant_id', true),
            'bootstrap-tenant'
        ) WHERE tenant_id IS NULL;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'statements_partitioned' AND column_name = 'tenant_id'
    ) THEN
        UPDATE statements_partitioned SET tenant_id = COALESCE(
            current_setting('app.tenant_id', true),
            'bootstrap-tenant'
        ) WHERE tenant_id IS NULL;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'outbox_events' AND column_name = 'tenant_id'
    ) THEN
        UPDATE outbox_events SET tenant_id = COALESCE(
            current_setting('app.tenant_id', true),
            'bootstrap-tenant'
        ) WHERE tenant_id IS NULL;
    END IF;
END $$;

ALTER TABLE plans ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE subscriptions ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE statements ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE statements_partitioned ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE outbox_events ALTER COLUMN tenant_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_plans_tenant_id ON plans (tenant_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_tenant_id ON subscriptions (tenant_id);
CREATE INDEX IF NOT EXISTS idx_statements_tenant_id ON statements (tenant_id);
CREATE INDEX IF NOT EXISTS idx_statements_part_tenant_id ON statements_partitioned (tenant_id);
CREATE INDEX IF NOT EXISTS idx_outbox_events_tenant_id ON outbox_events (tenant_id);

ALTER TABLE plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE statements ENABLE ROW LEVEL SECURITY;
ALTER TABLE statements_partitioned ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox_events ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS tenant_isolation_plans ON plans;
CREATE POLICY tenant_isolation_plans ON plans
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id')::text)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::text);

DROP POLICY IF EXISTS tenant_isolation_subscriptions ON subscriptions;
CREATE POLICY tenant_isolation_subscriptions ON subscriptions
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id')::text)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::text);

DROP POLICY IF EXISTS tenant_isolation_statements ON statements;
CREATE POLICY tenant_isolation_statements ON statements
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id')::text)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::text);

DROP POLICY IF EXISTS tenant_isolation_statements_partitioned ON statements_partitioned;
CREATE POLICY tenant_isolation_statements_partitioned ON statements_partitioned
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id')::text)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::text);

DROP POLICY IF EXISTS tenant_isolation_outbox_events ON outbox_events;
CREATE POLICY tenant_isolation_outbox_events ON outbox_events
    FOR ALL
    USING (tenant_id = current_setting('app.tenant_id')::text)
    WITH CHECK (tenant_id = current_setting('app.tenant_id')::text);
