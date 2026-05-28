-- Idempotency Keys table for tracking request state, concurrency locks, and caching responses
CREATE TABLE IF NOT EXISTS idempotency_keys (
    scope TEXT NOT NULL,
    key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'in_flight' CHECK (status IN ('in_flight', 'completed')),
    response_code INTEGER,
    response_body BYTEA,
    response_headers JSONB,
    payload_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope, key)
);

CREATE INDEX idx_idempotency_keys_expires_at ON idempotency_keys (expires_at);
