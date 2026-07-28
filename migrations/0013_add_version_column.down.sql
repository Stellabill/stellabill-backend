DROP TRIGGER IF EXISTS trg_plans_updated_at ON plans;
DROP TRIGGER IF EXISTS trg_subscriptions_updated_at ON subscriptions;

ALTER TABLE plans DROP COLUMN IF EXISTS updated_at;
ALTER TABLE plans DROP COLUMN IF EXISTS version;

ALTER TABLE subscriptions DROP COLUMN IF EXISTS updated_at;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS version;

DROP FUNCTION IF EXISTS update_timestamp();
