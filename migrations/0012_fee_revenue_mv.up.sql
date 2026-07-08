-- Materialized view aggregating fee/statement revenue by tenant (customer) and month.
--
-- Motivation:
--   The admin fee-history report previously scanned raw `statements` rows on every
--   request. This view pre-aggregates revenue so the report can read a small,
--   indexed result set instead of re-scanning the full table.
--
-- Dimensions:
--   - customer_id : the billing tenant identity (statements has no separate
--                   tenant_id column; customer_id is the tenant for billing).
--   - month       : the issuance month, truncated to the first day (UTC).
--
-- Source rows are restricted to active, revenue-bearing statements:
--   - deleted_at IS NULL  : exclude soft-deleted statements.
--   - archived_at IS NULL : archived rows have their amount/date nulled out
--                           (see migration 0010), so they cannot contribute.
--
-- `issued_at` and `total_amount` are stored as TEXT (RFC3339 / decimal string),
-- so we cast explicitly. Rows that fail to cast would error the refresh, so the
-- WHERE clause guards against NULLs that the archival stub leaves behind.

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_fee_revenue_monthly AS
SELECT
    s.customer_id                                              AS customer_id,
    date_trunc('month', (s.issued_at)::timestamptz)            AS month,
    s.currency                                                 AS currency,
    COUNT(*)                                                   AS statement_count,
    SUM((s.total_amount)::numeric)                             AS total_revenue
FROM statements s
WHERE s.deleted_at IS NULL
  AND s.archived_at IS NULL
  AND s.issued_at IS NOT NULL
  AND s.total_amount IS NOT NULL
GROUP BY s.customer_id, date_trunc('month', (s.issued_at)::timestamptz), s.currency
WITH NO DATA;

-- A UNIQUE index is REQUIRED for REFRESH MATERIALIZED VIEW CONCURRENTLY.
-- (customer_id, month, currency) is the natural grain of the aggregate.
CREATE UNIQUE INDEX IF NOT EXISTS uq_mv_fee_revenue_monthly
    ON mv_fee_revenue_monthly (customer_id, month, currency);

-- Secondary index to serve "by tenant, ordered by month" report queries.
CREATE INDEX IF NOT EXISTS idx_mv_fee_revenue_monthly_customer_month
    ON mv_fee_revenue_monthly (customer_id, month DESC);

-- Freshness metadata: a single-row table recording when the view was last
-- refreshed. The refresh worker updates this transactionally after each refresh
-- so the report can expose `last_refreshed_at` and decide stale-but-served.
CREATE TABLE IF NOT EXISTS mv_fee_revenue_refresh_state (
    id                 BOOLEAN     PRIMARY KEY DEFAULT TRUE,
    last_refreshed_at  TIMESTAMPTZ,
    -- Guard so the table can hold at most one row (id is always TRUE).
    CONSTRAINT mv_fee_revenue_refresh_state_singleton CHECK (id = TRUE)
);

-- Seed the singleton row with no refresh yet (NULL == "never refreshed").
INSERT INTO mv_fee_revenue_refresh_state (id, last_refreshed_at)
VALUES (TRUE, NULL)
ON CONFLICT (id) DO NOTHING;
