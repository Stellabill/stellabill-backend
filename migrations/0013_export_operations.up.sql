CREATE TABLE IF NOT EXISTS export_operations (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    caller_id TEXT NOT NULL,
    caller_roles JSONB NOT NULL DEFAULT '[]'::jsonb,
    status TEXT NOT NULL,
    result JSONB,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_export_operations_tenant_id ON export_operations (tenant_id);
CREATE INDEX IF NOT EXISTS idx_export_operations_updated_at ON export_operations (updated_at);
