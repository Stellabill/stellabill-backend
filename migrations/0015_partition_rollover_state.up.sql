-- 0015_partition_rollover_state.up.sql
--
-- This table tracks when the partition rollover job last ran, enabling
-- idempotent execution and preventing excessive partition management
-- operations. The job checks this table before scanning pg_catalog and
-- skips if last_rollover_at is within the cooldown period (1 hour).

CREATE TABLE IF NOT EXISTS partition_rollover_state (
    id          BOOLEAN PRIMARY KEY DEFAULT true,
    last_rollover_at  TIMESTAMPTZ NOT NULL DEFAULT 'epoch'::timestamptz,
    CONSTRAINT partition_rollover_state_singleton CHECK (id)
);

-- Seed the singleton row so the job can always UPDATE rather than
-- INSERT-or-UPDATE.
INSERT INTO partition_rollover_state (id, last_rollover_at)
VALUES (true, 'epoch'::timestamptz)
ON CONFLICT (id) DO NOTHING;
