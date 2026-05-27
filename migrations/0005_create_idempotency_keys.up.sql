-- Idempotency keys: durable backing store for the in-memory idempotency cache.
-- Each row binds an Idempotency-Key to the caller (scope), the request shape
-- (method + path + payload hash), and the cached response. Cross-scope reuse
-- is impossible because (scope, key) is the primary key.
CREATE TABLE IF NOT EXISTS idempotency_keys (
    scope         TEXT        NOT NULL,
    key           TEXT        NOT NULL,
    method        TEXT        NOT NULL,
    path          TEXT        NOT NULL,
    payload_hash  TEXT        NOT NULL,
    status_code   INTEGER     NOT NULL,
    response_body BYTEA       NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope, key)
);

-- Used by the periodic TTL sweeper.
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_expires_at
    ON idempotency_keys (expires_at);
