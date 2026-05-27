CREATE TABLE IF NOT EXISTS statements (
    id              TEXT        PRIMARY KEY,
    subscription_id TEXT        NOT NULL,
    customer_id     TEXT        NOT NULL,
    period_start    TEXT        NOT NULL,
    period_end      TEXT        NOT NULL,
    issued_at       TEXT        NOT NULL,
    total_amount    TEXT        NOT NULL,
    currency        TEXT        NOT NULL,
    kind            TEXT        NOT NULL,
    status          TEXT        NOT NULL,
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_statements_customer_id ON statements (customer_id);
CREATE INDEX IF NOT EXISTS idx_statements_subscription_id ON statements (subscription_id);
