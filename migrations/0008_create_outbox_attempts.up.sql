-- Tracks individual delivery attempts per outbox/webhook event (attempt timeline).
CREATE TABLE IF NOT EXISTS outbox_attempts (
    id             UUID                     PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id       UUID                     NOT NULL REFERENCES outbox_events(id) ON DELETE CASCADE,
    tenant_id      TEXT                     NOT NULL,
    attempt_number INT                      NOT NULL,
    response_code  INT,
    latency_ms     INT,
    -- Response body stored truncated to 4 KB; PII-scrubbed before insert.
    response_body  TEXT                     CHECK (octet_length(response_body) <= 4096),
    error_message  TEXT,
    next_retry_at  TIMESTAMP WITH TIME ZONE,
    attempted_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_outbox_attempts_event_id
    ON outbox_attempts (event_id, attempted_at DESC);
