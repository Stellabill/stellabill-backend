# Async Webhook Inbox

Inbound webhooks are acknowledged synchronously and processed asynchronously so
provider-side timeouts stay isolated from internal work.

## Flow

1. `WebhookVerification` middleware validates the signature and stashes
   `webhook_event_id`, `webhook_provider`, and `webhook_raw_body` on the Gin
   context.
2. `NewVerifiedWebhookHandler` persists the payload to `webhook_inbox` and
   returns **202 Accepted** (target: under 50 ms).
3. `worker.WebhookWorker` drains the inbox in creation order **per `source_id`**,
   publishing domain events to the outbox and recording
   `webhook_inbox_lag_seconds`.

## Deduplication

`webhook_inbox` has `UNIQUE (provider, provider_msg_id)`. Duplicate deliveries
from a provider are inserted with `ON CONFLICT DO NOTHING` and still receive
202, so retries are safe.

## Ordering

The worker only claims a pending row when no older pending/processing row
exists for the same `source_id`, preserving per-source delivery order.

## Schema

See `migrations/0014_add_webhook_inbox.up.sql`.

## Metrics

- `webhook_inbox_lag_seconds` — time from inbox insert to successful processing.
