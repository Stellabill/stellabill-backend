-- Create plan_templates table for merchant-defined plan templates
-- Supports deprecation workflow to prevent new subscriptions from using outdated plans
CREATE TABLE IF NOT EXISTS plan_templates (
    id TEXT PRIMARY KEY,
    merchant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    amount_cents BIGINT NOT NULL CHECK (amount_cents >= 0),
    currency TEXT NOT NULL CHECK (LENGTH(currency) = 3),
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds > 0),
    trial_seconds INTEGER NOT NULL DEFAULT 0 CHECK (trial_seconds >= 0),
    deprecated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(merchant_id, name)
);

-- Index for merchant lookups
CREATE INDEX idx_plan_templates_merchant_id ON plan_templates(merchant_id) WHERE deprecated_at IS NULL;

-- Index for deprecation queries
CREATE INDEX idx_plan_templates_deprecated_at ON plan_templates(deprecated_at);

-- Trigger to auto-update updated_at
CREATE OR REPLACE FUNCTION update_plan_templates_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER plan_templates_updated_at
    BEFORE UPDATE ON plan_templates
    FOR EACH ROW
    EXECUTE FUNCTION update_plan_templates_updated_at();
