-- 0008_statements_partition.up.sql

CREATE TABLE statements_partitioned (
    id              TEXT,
    subscription_id TEXT        NOT NULL,
    customer_id     TEXT        NOT NULL,
    period_start    TEXT        NOT NULL,
    period_end      TEXT        NOT NULL,
    issued_at       TEXT        NOT NULL,
    total_amount    TEXT        NOT NULL,
    currency        TEXT        NOT NULL,
    kind            TEXT        NOT NULL,
    status          TEXT        NOT NULL,
    deleted_at      TIMESTAMPTZ,
    PRIMARY KEY (period_start, id)
) PARTITION BY RANGE (period_start);

-- Create initial monthly partitions for statements
CREATE TABLE statements_p2023_12 PARTITION OF statements_partitioned FOR VALUES FROM ('2023-12-01T00:00:00Z') TO ('2024-01-01T00:00:00Z');
CREATE TABLE statements_p2024_01 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-01-01T00:00:00Z') TO ('2024-02-01T00:00:00Z');
CREATE TABLE statements_p2024_02 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-02-01T00:00:00Z') TO ('2024-03-01T00:00:00Z');
CREATE TABLE statements_p2024_03 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-03-01T00:00:00Z') TO ('2024-04-01T00:00:00Z');
CREATE TABLE statements_p2024_04 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-04-01T00:00:00Z') TO ('2024-05-01T00:00:00Z');
CREATE TABLE statements_p2024_05 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-05-01T00:00:00Z') TO ('2024-06-01T00:00:00Z');
CREATE TABLE statements_p2024_06 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-06-01T00:00:00Z') TO ('2024-07-01T00:00:00Z');
CREATE TABLE statements_p2024_07 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-07-01T00:00:00Z') TO ('2024-08-01T00:00:00Z');
CREATE TABLE statements_p2024_08 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-08-01T00:00:00Z') TO ('2024-09-01T00:00:00Z');
CREATE TABLE statements_p2024_09 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-09-01T00:00:00Z') TO ('2024-10-01T00:00:00Z');
CREATE TABLE statements_p2024_10 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-10-01T00:00:00Z') TO ('2024-11-01T00:00:00Z');
CREATE TABLE statements_p2024_11 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-11-01T00:00:00Z') TO ('2024-12-01T00:00:00Z');
CREATE TABLE statements_p2024_12 PARTITION OF statements_partitioned FOR VALUES FROM ('2024-12-01T00:00:00Z') TO ('2025-01-01T00:00:00Z');

-- Default partition for out of bounds
CREATE TABLE statements_p_default PARTITION OF statements_partitioned DEFAULT;

CREATE INDEX idx_statements_part_customer_id ON statements_partitioned (customer_id);
CREATE INDEX idx_statements_part_subscription_id ON statements_partitioned (subscription_id);

-- Gated by feature flag (conceptual swap in SQL, or manual execution step)
-- We will implement a one-shot copy job in Go or SQL. For safety in migration, we can copy data directly here.
-- The feature flag will be used in Go code.
