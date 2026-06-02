# DB connection pool at startup

## What this change does

`cmd/server/main.go` delegates all wiring to `routes.RegisterWithCleanup`, which
now opens a **real** pgx connection pool from `cfg.DBConn` at startup and injects
it into the handlers and the idempotency store. The returned cleanup function
closes the pool on graceful shutdown.

### Files

- **`internal/db/pool.go`** (new)
  - `NewPoolConfig(cfg)` — maps the validated `config.Config` `DBPool*` tuning
    fields onto a `*pgxpool.Config`:
    - `DBPoolMaxConns` → `MaxConns`
    - `DBPoolMinConns` → `MinConns`
    - `DBPoolMaxConnLifetime` (s) → `MaxConnLifetime`
    - `DBPoolMaxConnIdleTime` (s) → `MaxConnIdleTime`
    - `DBPoolHealthCheckPeriod` (s) → `HealthCheckPeriod`
    - `DBPoolConnectTimeout` (s) → `ConnConfig.ConnectTimeout` (per-dial)
  - `NewPool(ctx, cfg)` — builds the pool via `pgxpool.NewWithConfig` and
    **pings once** so startup fails fast against a dead database instead of
    serving traffic on a broken pool. Returns `(nil, nil)` when `cfg.DBConn`
    is empty (dev mode) so callers degrade to in-memory dependencies.
  - `PoolPinger` — adapter exposing `PingContext(ctx)` over
    `*pgxpool.Pool.Ping(ctx)`. **This is the fix that lights up readiness:**
    `*pgxpool.Pool` has `Ping`, but `handlers.DBPinger` requires `PingContext`,
    so a raw pool injected as the health dependency would never satisfy the type
    assertion in `handlers.(*Handler).getDatabase` and readiness would report
    `not_configured`.

- **`internal/routes/routes.go`**
  - Replaced `pgxpool.New(ctx, cfg.DBConn)` (which ignored every `DBPool*`
    field) with `db.NewPool(connectCtx, cfg)`, bounded by a context derived from
    `DBPoolConnectTimeout`.
  - Injects `&db.PoolPinger{Pool: dbPool}` as the handler health dependency
    instead of the raw pool. When no pool exists, a nil `handlers.DBPinger` is
    passed so readiness stays `not_configured` (no panic, no false "healthy").
  - Metrics goroutine and `NewPostgresIdempotencyStore` still receive the raw
    `*pgxpool.Pool` unchanged.

- **`internal/config/config.go`** (minimal unblock — see below)

## Security / correctness notes

- **Fail-fast ping** prevents a half-open pool from accepting traffic.
- **Connect timeout** is applied both as the startup context deadline and the
  per-dial `ConnectTimeout`, so an unreachable DB cannot hang boot indefinitely
  (covered by `TestNewPool_ConnectTimeout` using RFC 5737 TEST-NET-1).
- **Graceful dev mode**: empty `DATABASE_URL` ⇒ no pool, in-memory idempotency
  store, readiness `not_configured` — never a crash.
- **Pool exhaustion** is governed by `MaxConns`; pgx blocks acquires until a
  conn frees or the caller's context expires. Request handlers carry the gin
  request context, so an exhausted pool surfaces as a context-deadline error per
  request rather than a process-wide stall.
- No secrets are logged; the DSN is never printed (only wrapped error text).

## Tests

`internal/db/pool_test.go` (no live DB required):

- `TestNewPoolConfig_AppliesTuningFields` — every `DBPool*` field maps correctly,
  incl. `ConnectTimeout` onto `ConnConfig`.
- `TestNewPoolConfig_EmptyDBConn`, `TestNewPoolConfig_InvalidDBConn` — error paths.
- `TestNewPool_EmptyDBConnReturnsNilNil` — graceful dev-mode degradation.
- `TestNewPool_ConnectTimeout` — unreachable host fails fast (<5s), no leaked pool.
- `TestPoolPinger_NilPool` — nil-safety.
- `TestPoolPinger_SatisfiesDBPinger` — compile-time `DBPinger` shape assertion.

Run:

```
go test ./internal/db/ ./internal/config/ -count=1
# ok  stellarbill-backend/internal/db
# ok  stellarbill-backend/internal/config
```

## Pre-existing breakage blocking `go test ./...` (NOT part of this task)

The module did not compile at `main` before this change. `internal/config` was
repaired here because the pool work depends on it (duplicate const/func
declarations from a bad merge, a missing `validateAllowedOrigins`, and the
`Config` struct was missing the `DBPool*` fields its own tests reference). The
following remain broken in other packages and must be fixed by their owners
before the full suite can run:

| File | Error |
|------|-------|
| `internal/handlers/handler.go:67,80,90,97,107` | undefined `ErrorCodeInternal` / `ErrorCodeInvalidRequest` (constants defined nowhere) |
| `internal/middleware/auth.go:19` | `fmt.Sprintf("%ds", ttl)` (string) passed where `time.Duration` is expected by `auth.NewJWKSCache` |
| `internal/middleware/security.go:36` | `cfg.SecurityFrameAncestors` undefined on `config.Config` |
| `internal/middleware/request_signing.go:10` | `"io"` imported and not used |
| `internal/secrets/vault_provider_test.go:178` | undefined `errors` (missing import) |

Because `routes` imports `handlers` and `middleware`, and `cmd/server` imports
`routes`, those packages cannot build until the above are resolved — but the
pool wiring in `routes.go` itself is type-correct (`go build ./internal/routes/`
reports no errors originating in `routes.go`).
