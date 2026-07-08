-- Rollback the monthly fee revenue materialized view and its freshness metadata.
DROP TABLE IF EXISTS mv_fee_revenue_refresh_state;

DROP INDEX IF EXISTS idx_mv_fee_revenue_monthly_customer_month;
DROP INDEX IF EXISTS uq_mv_fee_revenue_monthly;

DROP MATERIALIZED VIEW IF EXISTS mv_fee_revenue_monthly;
