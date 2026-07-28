# HTTP client pool (`internal/httpx`)

`internal/httpx` provides a single shared `Pool` for all outbound HTTP
integrations (subscriber webhooks via the outbox, PagerDuty, and any future
integration). Each remote host gets its own connection budget, its own
circuit breaker, and its own DNS cache entry, so one unhealthy or
DNS-flapping upstream can't exhaust connections meant for the others or
keep talking to a decommissioned IP behind a stale A record.

## Why per-host, not one shared transport

A single `*http.Transport` shared across every outbound host has two
problems this package avoids:

- **Noisy-neighbor connections.** `MaxConnsPerHost` on a shared transport
  still applies per host, but `CloseIdleConnections` and circuit-breaking
  state are transport-wide. Giving every host its own transport, breaker,
  and DNS cache entry means a webhook subscriber having a bad day can't
  degrade delivery to anyone else.
- **Stale DNS.** Go's `http.Transport` dials once per new connection and
  then happily keeps reusing that connection for as long as it's alive —
  it never re-resolves DNS for an existing keep-alive connection. If an
  upstream fails over to a new IP, requests keep going to the old one
  until the connection breaks on its own. `Pool` re-resolves each host on
  a TTL and recycles that host's idle connections when the address changes.

## Configuration

```go
cfg := httpx.DefaultConfig()
cfg.MaxConnsPerHost = 32       // hard per-host connection budget
cfg.DNSTTL = 15 * time.Second  // 0 = re-resolve on every dial
cfg.CircuitMaxFailures = 5     // consecutive transport failures to trip

pool := httpx.New(cfg)
```

| Field | Default | Notes |
| --- | --- | --- |
| `MaxConnsPerHost` | 64 | Total connections (dialing + active + idle) a host may hold. |
| `MaxIdleConnsPerHost` | 16 | Idle (keep-alive) connections kept warm per host. |
| `IdleConnTimeout` | 90s | Idle connections older than this are closed. |
| `DialTimeout` | 5s | Per-dial timeout. |
| `RequestTimeout` | 10s | Whole request/response timeout. |
| `DNSTTL` | 30s | How long a resolved address is trusted. **0 re-resolves on every dial.** |
| `CircuitMaxFailures` | 5 | Consecutive transport-level failures before a host's breaker opens. |
| `CircuitOpenTimeout` | 30s | How long the breaker stays open before a half-open probe. |
| `CircuitHalfOpenMax` | 1 | Probe requests allowed through while half-open. |
| `Resolver` | `net.DefaultResolver` | Anything with `LookupHost(ctx, host) ([]string, error)`. |
| `Clock` | wall clock | Swap in tests to control TTL expiry deterministically. |

Zero-valued fields fall back to `DefaultConfig()`, **except `DNSTTL`**,
which is a legitimate zero value (see below).

## Per-host connection budget

`Pool` lazily creates one `*http.Transport` per host the first time it's
dialed, with `MaxConnsPerHost`/`MaxIdleConnsPerHost` applied to that host
alone. Go's transport blocks (rather than errors) extra requests until a
connection slot frees up, so bursts queue instead of failing outright.

## Per-host circuit breaker

Each host also gets its own [`gobreaker.CircuitBreaker`](https://github.com/sony/gobreaker)
(the same library `internal/db` already uses for the database pool). Only
transport-level failures — dial errors, timeouts, DNS errors — count
against the breaker; a `4xx`/`5xx` HTTP response is not itself a breaker
failure, matching `http.Client.Do`'s own success/error contract so `Do`'s
return values stay conventional (non-nil `*http.Response` with a nil
error whenever the round trip completed, regardless of status code).

When a host's breaker is open, `Do` returns immediately with an error
wrapping `httpx.ErrCircuitOpen`, without touching the network or DNS.

## DNS refresh

`Pool` resolves each host through `Config.Resolver` and dials the
resolved address directly (not the hostname), caching the result for
`DNSTTL`. On every dial:

- If the cached entry is younger than `DNSTTL`, it's reused — no lookup.
- If it's stale (or `DNSTTL` is `0`, refresh-every-request), the pool
  re-resolves. If the resolved address changed, the host's idle
  connections are closed so new requests establish a fresh connection to
  the new address instead of continuing to reuse an existing connection to
  the address that just disappeared.
- If resolution fails, the pool falls back to the standard library's own
  dialer/resolver for that one dial rather than failing the host outright.

`DNSTTL: 0` is the documented "refresh every request" mode for hosts
behind aggressive DNS failover, at the cost of a DNS lookup per request.

## Metrics

`http_client_conn_reuse_ratio{host}` (gauge, 0–1) tracks the fraction of
requests per host that reused a pooled connection instead of dialing a new
one, via `httptrace.ClientTrace.GotConn`. A ratio that stays near zero for
a host under steady load usually means connections are being recycled too
aggressively (DNS TTL too low, idle timeout too short) or the upstream is
closing them itself.

## Usage

`Pool.Do(req *http.Request) (*http.Response, error)` satisfies the small
`Do(req) (*http.Response, error)` interface most HTTP-based integrations
already accept, so a `*httpx.Pool` can usually be passed in directly:

```go
pool := httpx.New(httpx.DefaultConfig())

// internal/integrations/pagerduty
pdClient := pagerduty.NewWithHTTP(routingKey, "https://events.pagerduty.com/v2/enqueue", pool)
```

For integrations with a different client shape (e.g. the outbox's
`Post(ctx, url, contentType, body) (int, error)`), wrap the pool in a small
adapter — see `outbox.PooledHTTPClient` in
[`internal/outbox/pooled_client.go`](../internal/outbox/pooled_client.go).

Both `internal/integrations/pagerduty` and `internal/outbox` default to a
package-level shared `Pool` when constructed without one explicitly
(`pagerduty.New`, `outbox.NewService` with `ServiceConfig.HTTPPool` unset),
so existing call sites get per-host budgets, breakers, and DNS-TTL
refresh for free.

## Testing

`internal/httpx/client_test.go` covers the per-host connection budget
under concurrent load, circuit breaker tripping and short-circuiting, the
conn-reuse-ratio metric, and the DNS TTL behaviors above (including the
zero-TTL "refresh every request" edge case) using fake `Clock`/`Resolver`
implementations — no real network or DNS involved. Run:

```
go test ./internal/httpx/...
```
