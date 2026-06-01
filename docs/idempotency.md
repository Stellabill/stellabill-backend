# Idempotency Keys

Stellabill supports the `Idempotency-Key` request header on mutation endpoints
(`POST`, `PUT`, `PATCH`, `DELETE`) so clients can safely retry under network
timeouts, transient errors, or unclear connection state without producing
duplicate side effects.

## Quick reference

| Aspect            | Value                                                      |
| ----------------- | ---------------------------------------------------------- |
| Header            | `Idempotency-Key: <opaque string, max 255 chars>`          |
| Scope             | Per authenticated caller (`tenantID` + `callerID`)         |
| Methods covered   | `POST`, `PUT`, `PATCH`, `DELETE`                           |
| Methods skipped   | `GET`, `HEAD`, `OPTIONS`                                   |
| TTL               | 24 hours (default)                                         |
| Replay indicator  | `Idempotency-Replayed: true` on the response               |
| Mismatch response | `422 Unprocessable Entity`                                 |
| Oversized key     | `400 Bad Request`                                          |

## Endpoints that honor the header

The middleware is installed on the `/api/v1/*` group, immediately after the
JWT authentication middleware. Any `POST`, `PUT`, `PATCH`, or `DELETE` route
registered under `/api/v1` automatically honors the header.

The legacy `/api/*` group is intentionally **not** covered: it authenticates
per-route rather than at the group level, so installing idempotency at the
group level would run it before authentication and the per-caller scope
would be untrusted. New mutation endpoints should be registered under
`/api/v1`.

Read endpoints (e.g. `GET /api/v1/subscriptions`) ignore the header and are
never cached through this mechanism.

## Behavior

1. **First request with a key** — the request is processed normally and, if
   the response status is 2xx, the response (status code + body) is stored
   alongside the request's payload hash, HTTP method, and path.
2. **Retry with the same key, same caller, same payload, same route** — the
   stored response is returned verbatim and the response carries
   `Idempotency-Replayed: true`. The downstream handler is *not* invoked, so
   no additional side effects occur.
3. **Retry with the same key but a different payload, method, or path** — the
   request is rejected with `422 Unprocessable Entity` and the body
   `{"error":"Idempotency-Key reused with a different request"}`. This protects
   clients from accidentally reusing a key for a logically different operation.
4. **Concurrent retries with the same key** — only the first request runs the
   handler. Subsequent concurrent retries wait up to 10 seconds for the first
   to complete and then receive the cached response. If the first errors,
   one of the waiters proceeds normally.
5. **Failed responses (non-2xx)** — never cached. Clients can safely retry
   after a server error and the next attempt will execute the handler.
6. **Expired entries** — entries older than the TTL are evicted by a periodic
   sweeper. A retry after expiry executes the handler again.

## Security properties

- **Cross-caller isolation.** Cached entries are namespaced by
  `tenantID + callerID`. Two callers using the same `Idempotency-Key` value
  cannot read each other's cached responses. The middleware is installed
  *after* authentication so the scope is derived from a verified identity,
  not a client-supplied header.
- **Method + path binding.** The cached entry records the original HTTP
  method and path. Replaying the same key against a different route returns
  `422`, preventing key reuse from silently triggering a different operation.
- **Payload binding.** The cached entry records a SHA-256 hash of the
  request body. Replaying the same key with a modified payload returns
  `422` rather than producing inconsistent results.
- **Length cap.** Keys longer than 255 characters are rejected with `400`.
- **No key, no caching.** Requests without the header are processed
  normally and never recorded.
- **Anonymous requests** are placed in a single `anonymous` scope. Mutation
  endpoints in Stellabill require authentication, so this branch is reachable
  only for misconfigured routes; do not rely on idempotency on unauthenticated
  routes.

## Storage and TTL

The default backing store is in-process and thread-safe. A migration
(`migrations/004_create_idempotency_keys.up.sql`) defines an
`idempotency_keys` table that mirrors the in-memory shape:

| Column          | Purpose                                          |
| --------------- | ------------------------------------------------ |
| `scope`         | Caller namespace (tenant + subject).             |
| `key`           | Raw `Idempotency-Key` value.                     |
| `method`, `path`| Bound request shape.                             |
| `payload_hash`  | SHA-256 hash of the request body.                |
| `status_code`   | Cached response status.                          |
| `response_body` | Cached response body bytes.                      |
| `created_at`    | Wall-clock time the entry was written.           |
| `expires_at`    | TTL expiry; sweeper deletes rows past this time. |

`(scope, key)` is the primary key, which is what enforces cross-caller
isolation at the database level: two callers using the same key write to
different rows.

## Client guidance

- Generate keys with a high-entropy source (UUIDv4 or 128-bit random hex).
- Reuse the *same* key for retries of the *same* logical request only.
- Do not reuse keys across different operations (different payloads, routes,
  or HTTP methods); doing so yields `422`.
- Do not rely on idempotency for non-2xx responses — retry, but expect the
  handler to run again.

## Example

```http
POST /api/v1/subscriptions HTTP/1.1
Authorization: Bearer <token>
X-Tenant-ID: tenant-1
Idempotency-Key: 8e7b1f1c-a1d6-4a9d-9b0a-2dca6f5b2a17
Content-Type: application/json

{"plan_id":"plan_basic","customer":"cust_42"}
```

A successful first response (e.g. `201 Created`) is cached. Replaying the
exact same request returns:

```http
HTTP/1.1 201 Created
Idempotency-Replayed: true
Content-Type: application/json; charset=utf-8

{"id":"sub_…","plan_id":"plan_basic","customer":"cust_42",…}
```
