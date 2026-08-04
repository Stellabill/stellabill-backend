# Cross-Region Write Forwarding

When running an active–passive multi-region deployment, the **passive** region
must not accept mutating requests against a local writable database. Instead it
forwards (or redirects) writes to the **active** region so failover data-loss
windows stay small.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `REGION_ROLE` | `active` | `active` serves writes locally; `passive` forwards/redirects writes |
| `ACTIVE_REGION_URL` | _(empty)_ | Base URL of the active region (required when `REGION_ROLE=passive`) |
| `REGION_FORWARD_MODE` | `proxy` | `proxy` reverse-proxies the write; `redirect` returns HTTP 302 |
| `REGION_FORWARD_AUTH_TOKEN` | _(empty)_ | Optional Bearer token attached when proxying to the active region |

## Behaviour

- **Read methods** (`GET`, `HEAD`, `OPTIONS`) always pass through locally.
- **Write methods** (`POST`, `PUT`, `PATCH`, `DELETE`) on a passive region:
  - **proxy**: reverse-proxy to `ACTIVE_REGION_URL` + path/query, stamp
    `X-Region-Hop: 1`, and return the upstream status/body.
  - **redirect**: respond with `302 Found` and a `Location` pointing at the
    active URL.
- **Loop prevention**: if an inbound write already carries `X-Region-Hop: 1`,
  the middleware returns `508 Loop Detected` and does not forward again.
- **Active unreachable**: proxy mode returns `503 Service Unavailable` with
  `error: active_region_unavailable` when the active region cannot be reached
  or `ACTIVE_REGION_URL` is unset.

## Tracing

Forwarded writes create a span `region.write_forward` with attributes:

- `region.role`
- `region.forward_mode`
- `region.forwarded=true`
- `region.active_url`

## Example

```bash
# Passive region
REGION_ROLE=passive
ACTIVE_REGION_URL=https://api-active.example.com
REGION_FORWARD_MODE=proxy
REGION_FORWARD_AUTH_TOKEN=shared-tunnel-secret
```
