-- Contract Events: normalized read model for ingested contract events.
CREATE TABLE IF NOT EXISTS contract_events (
    id              TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    event_type      TEXT NOT NULL,
    contract_id     TEXT NOT NULL,
    tenant_id       TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}',
    occurred_at     TIMESTAMPTZ NOT NULL,
    ingested_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    sequence_num    BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'processed'
        CHECK (status IN ('processed', 'skipped', 'failed')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_contract_events_contract_id ON contract_events (contract_id);
CREATE INDEX idx_contract_events_tenant_id ON contract_events (tenant_id);
CREATE INDEX idx_contract_events_event_type ON contract_events (event_type);
CREATE INDEX idx_contract_events_occurred_at ON contract_events (occurred_at);
CREATE INDEX idx_contract_events_idempotency ON contract_events (idempotency_key);
