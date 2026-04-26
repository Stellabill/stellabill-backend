-- Subscriptions indexes
CREATE INDEX IF NOT EXISTS idx_subscriptions_customer_status
ON subscriptions (customer, status);

CREATE INDEX IF NOT EXISTS idx_subscriptions_next_billing
ON subscriptions (next_billing);

CREATE INDEX IF NOT EXISTS idx_subscriptions_plan_id
ON subscriptions (plan_id);

-- Plans indexes
CREATE INDEX IF NOT EXISTS idx_plans_name
ON plans (name);

-- Statements indexes (if table exists)
CREATE INDEX IF NOT EXISTS idx_statements_subscription_created
ON statements (subscription_id, created_at DESC);

-- Reconciliation indexes (if table exists)
CREATE INDEX IF NOT EXISTS idx_reconciliation_status_created
ON reconciliation (status, created_at DESC);