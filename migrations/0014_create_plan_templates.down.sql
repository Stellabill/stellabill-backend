-- Drop plan_templates table and associated objects
DROP TRIGGER IF EXISTS plan_templates_updated_at ON plan_templates;
DROP FUNCTION IF EXISTS update_plan_templates_updated_at();
DROP INDEX IF EXISTS idx_plan_templates_deprecated_at;
DROP INDEX IF EXISTS idx_plan_templates_merchant_id;
DROP TABLE IF EXISTS plan_templates;
