-- Saga coordinator tables for cross-aggregate billing workflows.
-- Each saga orchestrates steps with compensating actions on failure.
CREATE TABLE IF NOT EXISTS saga_instances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    context JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_saga_instances_status
    ON saga_instances(status)
    WHERE status = 'running';

CREATE TABLE IF NOT EXISTS saga_step_results (
    saga_id UUID NOT NULL REFERENCES saga_instances(id),
    step_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT,
    executed_at TIMESTAMPTZ,
    compensated_at TIMESTAMPTZ,
    PRIMARY KEY (saga_id, step_key)
);
