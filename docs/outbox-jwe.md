# Outbox JWE Encryption

Sensitive outbox event payloads (for example `webhook.received` and `payment.processed`) are encrypted with JSON Web Encryption (JWE) using subscriber-supplied public keys before they are stored and published. Transport-layer HTTPS alone does not protect data at rest in the outbox table or in downstream log pipelines.

## Envelope format

Encrypted events store a compact JWE string in `event_data`:

```json
{
  "type": "webhook.received",
  "id": "evt-uuid",
  "timestamp": "2026-06-23T12:00:00Z",
  "encrypted": true,
  "jwe": "eyJ...",
  "key_id": "subscriber-key-2026-06",
  "subscriber_id": "sub-123"
}
```

Published HTTP deliveries use `Content-Type: application/jose+json` with the compact JWE as the body.

Algorithms:

- Key encryption: `RSA-OAEP-256`
- Content encryption: `A256GCM`

## Subscriber key registration

Subscribers register a public JWK through admin endpoints (RBAC: `manage:subscriptions`):

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/admin/subscriber-keys` | Register a new JWK |
| GET | `/api/admin/subscriber-keys/:subscriber_id` | List keys for a subscriber |
| GET | `/api/admin/subscriber-keys/id/:id` | Fetch one key record |
| PATCH | `/api/admin/subscriber-keys/:id` | Revoke, expire, or re-activate a key |

Keys are stored in the `subscriber_keys` table. Only `active` keys that are not past `expires_at` are used for encryption.

## Decryption flow (subscriber)

1. Receive the HTTP POST with `application/jose+json`.
2. Load the private key that matches `key_id` from your key store.
3. Decrypt the compact JWE using `RSA-OAEP-256` / `A256GCM`.
4. Parse the inner JSON payload (`id`, `type`, `data`, `occurred_at`, ...).

Go example with `github.com/lestrrat-go/jwx/v2`:

```go
plaintext, err := jwe.Decrypt([]byte(compact), jwe.WithKey(jwa.RSA_OAEP_256(), privateJWK))
```

## Missing or invalid keys

If a sensitive event cannot be encrypted because:

- no `subscriber_id` is present,
- no active JWK is registered, or
- the only available key is revoked or expired,

the publish attempt is treated as a **permanent failure** and the event is routed directly to the dead-letter queue (`status = failed`) without retries.

## Key rotation

Register a new key with a new `key_id`. Revoke the previous key via `PATCH /api/admin/subscriber-keys/:id`. Pending events pick up the latest active key at publish time, so mid-batch rotation does not require re-encrypting stored rows.

## Configuration

```bash
OUTBOX_JWE_ENABLED=true
OUTBOX_JWE_SENSITIVE_EVENT_TYPES=webhook.received,payment.processed
```

## Webhook ingestion

Webhook handlers should pass `X-Subscriber-ID` so `webhook.received` events can be associated with the correct encryption key.
