-- 0014_create_cdc_publication.up.sql
-- Creates a logical replication publication for CDC streaming.
-- Uses column-level allowlists to exclude PII columns from the stream.
--
-- The publication is named "stellabill_cdc" and uses the pgoutput plugin,
-- which is the standard PostgreSQL logical decoding plugin compatible with
-- pgx replication consumers and Kafka Connect Debezium.

-- Create the publication (pgoutput is a built-in output plugin, no extension needed) with column allowlists that exclude PII columns.
-- PII columns excluded:
--   subscriptions.customer       – customer name/identifier
--   statements.customer_id       – customer identifier
--   statements_partitioned.customer_id – customer identifier
--   outbox_events.event_data     – raw JSON blob may contain PII
--   outbox_attempts.response_body – may contain PII
--   contract_events.payload      – raw JSONB may contain PII
--   idempotency_keys.response_body – cached API responses
--   subscriber_keys.jwk          – cryptographic key material
--   export_operations.caller_id  – caller identity
--   notification_preferences.*   – user notification settings (PII-adjacent)
--   saga_instances.context       – JSONB may contain PII

CREATE PUBLICATION stellabill_cdc FOR TABLE
    plans (
        id, name, amount_cents, currency, interval, description,
        created_at, updated_at, version
    ),
    subscriptions (
        id, plan_id, status, amount_cents, interval,
        next_billing, created_at, updated_at, version
    ),
    statements (
        id, subscription_id, period_start, period_end,
        issued_at, total_amount, currency, kind, status,
        deleted_at, archived_at, archive_key
    ),
    outbox_events (
        id, event_type, aggregate_id, aggregate_type,
        occurred_at, status, retry_count, max_retries,
        next_retry_at, error_message, created_at, updated_at,
        version, deduplication_id
    ),
    outbox_attempts (
        id, event_id, tenant_id, attempt_number,
        response_code, latency_ms,
        next_retry_at, attempted_at
    ),
    outbox_publisher_progress (
        publisher, last_event_id, updated_at
    ),
    contract_events (
        id, idempotency_key, event_type, contract_id,
        tenant_id, occurred_at, ingested_at, sequence_num,
        status, created_at
    ),
    export_operations (
        id, tenant_id, caller_roles, status,
        result, error, created_at, updated_at
    ),
    saga_instances (
        id, name, status, created_at, updated_at
    ),
    saga_step_results (
        saga_id, step_key, status,
        executed_at, compensated_at
    )
WITH (publish = 'insert,update,delete');
