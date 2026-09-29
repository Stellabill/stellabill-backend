-- 0016_plan_baselines.up.sql
--
-- Stores periodic pg_stat_statements snapshots that can later be compared to
-- detect query-plan regressions.

CREATE TABLE IF NOT EXISTS plan_baselines (
    id BIGSERIAL PRIMARY KEY,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    query_text TEXT NOT NULL,
    mean_time DOUBLE PRECISION NOT NULL,
    total_time DOUBLE PRECISION NOT NULL,
    calls BIGINT NOT NULL,
    shared_blks_read BIGINT NOT NULL DEFAULT 0,
    shared_blks_hit BIGINT NOT NULL DEFAULT 0,
    shared_blks_dirtied BIGINT NOT NULL DEFAULT 0,
    scan_type TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS plan_baselines_captured_at_idx ON plan_baselines (captured_at DESC);
CREATE INDEX IF NOT EXISTS plan_baselines_query_text_idx ON plan_baselines (query_text);
