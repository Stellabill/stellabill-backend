-- Subscriber public keys for JWE encryption of sensitive outbox payloads.
CREATE TABLE IF NOT EXISTS subscriber_keys (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscriber_id TEXT        NOT NULL,
    key_id        TEXT        NOT NULL,
    jwk           JSONB       NOT NULL,
    status        TEXT        NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active', 'revoked', 'expired')),
    expires_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (subscriber_id, key_id)
);

CREATE INDEX IF NOT EXISTS idx_subscriber_keys_active
    ON subscriber_keys (subscriber_id, created_at DESC)
    WHERE status = 'active';

CREATE OR REPLACE FUNCTION update_subscriber_keys_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_subscriber_keys_updated_at
    BEFORE UPDATE ON subscriber_keys
    FOR EACH ROW
    EXECUTE FUNCTION update_subscriber_keys_updated_at();
