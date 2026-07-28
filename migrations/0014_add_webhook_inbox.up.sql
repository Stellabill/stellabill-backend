CREATE TABLE webhook_inbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider VARCHAR(100) NOT NULL,
    provider_msg_id VARCHAR(255) NOT NULL,
    source_id VARCHAR(255) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    error_text TEXT,
    attempts INT NOT NULL DEFAULT 0,
    
    UNIQUE(provider, provider_msg_id)
);

CREATE INDEX idx_webhook_inbox_pending ON webhook_inbox (status, created_at ASC);
CREATE INDEX idx_webhook_inbox_source ON webhook_inbox (source_id);