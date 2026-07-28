# PgBouncer Sidecar — Setup and Operations Guide

PgBouncer is deployed as a sidecar container alongside the API pod. It uses
**transaction pooling** to absorb connection storms during pod restarts and to
allow many application goroutines to share a small number of Postgres backend
connections.

---

## Table of contents

- [Why PgBouncer?](#why-pgbouncer)
- [Architecture](#architecture)
- [Configuration reference](#configuration-reference)
- [Connection lifetime overrides](#connection-lifetime-overrides)
- [Prepared statements and DB_STATEMENT_CACHE_MODE](#prepared-statements-and-db_statement_cache_mode)
- [Deploying with Kustomize](#deploying-with-kustomize)
- [Deploying with Helm](#deploying-with-helm)
- [Credentials management](#credentials-management)
- [Monitoring and observability](#monitoring-and-observability)
- [Troubleshooting](#troubleshooting)
- [Security considerations](#security-considerations)

---

## Why PgBouncer?

Postgres creates a OS process per backend connection. At high pod counts or
during rolling restarts, every pod connects simultaneously and the connection
count spikes. With `max_connections=100` and 10 pods each holding 25 idle
connections, you hit the ceiling without serving a single request.

PgBouncer solves this by:

- Multiplexing many application-side connections onto few Postgres backend connections.
- Queuing connection requests during restarts rather than refusing them.
- Recycling backend connections on a configurable lifetime, preventing stale TCP sessions.

---

## Architecture

```
┌──────────────────────────────────────────┐
│  Kubernetes Pod                          │
│                                          │
│  ┌────────────────────┐                  │
│  │  stellabill-api    │                  │
│  │  (Go application)  │─── PGBOUNCER_ENABLED=true
│  │                    │─── PGBOUNCER_HOST=127.0.0.1
│  │  DATABASE_URL      │─── PGBOUNCER_PORT=5432      │
│  │  points to         │─── DB_STATEMENT_CACHE_MODE= │
│  │  real Postgres     │    describe                 │
│  └──────────┬─────────┘                  │
│             │ connects to 127.0.0.1:5432 │
│             ▼                            │
│  ┌────────────────────┐                  │
│  │  pgbouncer         │                  │
│  │  (sidecar)         │                  │
│  │  pool_mode=        │──── egress to Postgres backend
│  │  transaction       │     (host/port from Secret)
│  └────────────────────┘                  │
└──────────────────────────────────────────┘
```

Key properties:

- PgBouncer binds to `127.0.0.1:5432` (loopback). No other pod can reach it.
- The application's `DATABASE_URL` still points to real Postgres (used for user/dbname/sslmode); the host and port are overridden at runtime by `PGBOUNCER_HOST` and `PGBOUNCER_PORT`.
- Backend credentials are written to `/etc/pgbouncer/userlist.txt` by an init container and are never stored in the ConfigMap.

---

## Configuration reference

| Environment variable | Default | Description |
|---|---|---|
| `PGBOUNCER_ENABLED` | `false` | Route connections through the sidecar |
| `PGBOUNCER_HOST` | `127.0.0.1` | Sidecar listen address |
| `PGBOUNCER_PORT` | `5432` | Sidecar listen port |
| `DB_STATEMENT_CACHE_MODE` | `prepare` | pgx query-exec mode (`describe` required for transaction pooling) |
| `PGBOUNCER_IDLE_IN_TX_TIMEOUT` | `30` | Seconds before an idle-in-transaction connection is aborted (written into pgbouncer.ini) |

All values have safe defaults and are validated at startup. Invalid values emit
a warning and fall back to the default rather than refusing to start.

### pgbouncer.ini tuning knobs

| Setting | Default | Description |
|---|---|---|
| `pool_mode` | `transaction` | Pooling mode. Must be `transaction` for this setup. |
| `max_client_conn` | `100` | Max application-side connections (queue excess rather than refuse) |
| `default_pool_size` | `25` | Backend connections per (user, db) pair |
| `min_pool_size` | `2` | Connections kept open when idle |
| `reserve_pool_size` | `5` | Extra connections for burst |
| `server_idle_timeout` | `600` | Close idle backend connections after N seconds |
| `server_lifetime` | `3600` | Recycle backend connections after N seconds |
| `query_wait_timeout` | `120` | Cancel waiting client after N seconds |
| `idle_transaction_timeout` | `30` | Abort idle-in-transaction connections after N seconds |

---

## Connection lifetime overrides

PgBouncer enforces connection lifetime at two levels. The Go connection pool
enforces it at a third level. These values should be set in concert.

### Go pool (`internal/db/pool.go`)

| Config field | Env var | Default | Purpose |
|---|---|---|---|
| `DBPoolMaxConnLifetime` | `DB_POOL_MAX_CONN_LIFETIME` | `3600 s` | Recycle pool connections after this duration |
| `DBPoolMaxConnIdleTime` | `DB_POOL_MAX_CONN_IDLE_TIME` | `600 s` | Evict idle pool connections after this duration |
| `DBPoolConnectTimeout` | `DB_POOL_CONNECT_TIMEOUT` | `5 s` | Per-dial timeout to the sidecar |

### PgBouncer (`pgbouncer.ini`)

| Setting | Relationship to Go pool |
|---|---|
| `server_lifetime` | Should be ≤ `DB_POOL_MAX_CONN_LIFETIME`. When PgBouncer recycles a backend connection the Go pool will re-dial and get a new one. |
| `server_idle_timeout` | Should be < `DB_POOL_MAX_CONN_IDLE_TIME` and < any firewall/NAT idle-timeout (typically 300–900 s for cloud VPCs). |
| `idle_transaction_timeout` | Matches `PGBOUNCER_IDLE_IN_TX_TIMEOUT`. Set this shorter than any application transaction timeout to surface stale transactions early. |

### Recommended production values

```
# Postgres: max_connections=100, 4 pods

# pgbouncer.ini
default_pool_size = 20        # 100/4 = 25, leave 5 for DBA/admin
max_client_conn = 200
server_lifetime = 3600        # 1 hour
server_idle_timeout = 600     # 10 min — well below typical NAT timeout
idle_transaction_timeout = 30 # 30 s — short enough to catch runaway transactions

# Environment variables (Go app)
DB_POOL_MAX_CONN_LIFETIME=3600
DB_POOL_MAX_CONN_IDLE_TIME=600
DB_POOL_CONNECT_TIMEOUT=5
PGBOUNCER_IDLE_IN_TX_TIMEOUT=30
```

### Long-running transactions

Transaction pooling returns a backend connection to the pool at `COMMIT` or
`ROLLBACK`. If a transaction is genuinely long-running (e.g. batch data export),
the backend connection is held for its full duration — this is expected behaviour.

What transaction pooling prevents is holding a backend connection *between*
transactions while the application does unrelated work (common in ORM-heavy
code). The `idle_transaction_timeout` in pgbouncer.ini is your safety net: any
`BEGIN` that sits idle longer than this value is forcibly rolled back, freeing
the backend connection.

For long-running transactions specifically:

- Ensure `idle_transaction_timeout` is set high enough that legitimate long
  transactions are not aborted (or set `0` for those sessions).
- Consider routing batch/export operations through a direct connection that
  bypasses PgBouncer (add a separate `DATABASE_DIRECT_URL` env var pointing
  to Postgres and use it for those jobs).

---

## Prepared statements and DB_STATEMENT_CACHE_MODE

Transaction pooling is incompatible with Postgres named prepared statements
because a backend connection is recycled between transactions — the next client
may get a different backend that has no knowledge of statements prepared by a
previous client, causing:

```
ERROR: prepared statement "stmtcache_1" does not exist
```

The Go pool (`pgx`) solves this via `DB_STATEMENT_CACHE_MODE`:

| Value | pgx mode | PgBouncer compatibility |
|---|---|---|
| `describe` | `QueryExecModeDescribeExec` | ✅ Required for transaction pooling |
| `simple` | `QueryExecModeSimpleProtocol` | ✅ Compatible |
| `prepare` | `QueryExecModeCacheStatement` | ❌ Not compatible with transaction pooling |

**Always set `DB_STATEMENT_CACHE_MODE=describe` when `PGBOUNCER_ENABLED=true`.**

The config validator emits a warning if you enable PgBouncer but leave the
mode as `prepare`. This will not stop the server from starting, but queries
will fail at runtime.

### What `describe` mode does

Instead of sending `Parse` (which registers a named prepared statement on the
backend connection), pgx sends `DescribeStatement` to infer parameter types.
No named statement is left on the backend, so there is nothing to lose when
the backend connection is recycled.

### Named-safe query patterns

If you need to write Go code that runs under `describe` mode:

```go
// Use $1, $2, ... positional placeholders (works in all modes)
rows, err := pool.Query(ctx, "SELECT * FROM plans WHERE id = $1", planID)

// sql.Named is for database/sql, not pgx — use positional placeholders
// NOT: pgx.NamedArgs{"id": planID}  (this requires prepare mode)
```

---

## Deploying with Kustomize

The base manifests live in `deploy/kustomize/base/`. Overlays in
`deploy/kustomize/overlays/` customize replica counts and pool sizes.

```bash
# Preview the production overlay
kubectl kustomize deploy/kustomize/overlays/production

# Apply to the production namespace
kubectl apply -k deploy/kustomize/overlays/production
```

Before applying, create the Secret with your real credentials:

```bash
kubectl create secret generic pgbouncer-credentials \
  --namespace stellabill-production \
  --from-literal=db-host=postgres.db.svc.cluster.local \
  --from-literal=db-port=5432 \
  --from-literal=db-name=stellabill \
  --from-literal=db-user=app \
  --from-literal=db-password='<password>' \
  --from-literal=database-url='postgres://app:<password>@postgres.db.svc.cluster.local:5432/stellabill?sslmode=require'
```

Or use External Secrets Operator / Vault Agent to sync the Secret from your
secrets manager.

### Overlay customisation

Production and staging overlays patch `pool_mode`, pool sizes, and timeouts
via the `patch-pgbouncer-config.yaml` files. To add a new environment (e.g.
`preview`), copy `overlays/staging` and adjust the namespace, replica count,
and pool size.

---

## Deploying with Helm

```bash
# Install with PgBouncer enabled
helm upgrade --install stellabill deploy/helm/stellabill \
  --namespace stellabill-production \
  --set pgbouncer.enabled=true \
  --set pgbouncer.defaultPoolSize=20 \
  --set pgbouncer.maxClientConn=200

# Dry run first
helm upgrade --install stellabill deploy/helm/stellabill \
  --namespace stellabill-production \
  --set pgbouncer.enabled=true \
  --dry-run --debug
```

Key Helm values (see `deploy/helm/stellabill/values.yaml` for all defaults):

```yaml
pgbouncer:
  enabled: true
  credentialsSecret: pgbouncer-credentials  # Secret must exist before install
  listenAddr: "127.0.0.1"
  listenPort: 5432
  poolMode: transaction
  defaultPoolSize: 25
  maxClientConn: 100
  idleTransactionTimeout: 30
  serverLifetime: 3600
  serverIdleTimeout: 600
```

When `pgbouncer.enabled: false` (the default), no PgBouncer containers or
ConfigMaps are rendered, and the application connects directly to Postgres.

---

## Credentials management

The pgbouncer-credentials Secret holds:

| Key | Description |
|---|---|
| `db-host` | Postgres hostname |
| `db-port` | Postgres port |
| `db-name` | Database name |
| `db-user` | Database user |
| `db-password` | Database password (used to generate `userlist.txt`) |
| `database-url` | Full DSN for the Go app (`DATABASE_URL`) |

**Never commit real credentials.** The base Secret manifest contains
placeholder `base64`-encoded values (`placeholder`). Replace them via your
secrets manager or `kubectl create secret` before deploying.

### Init container

An init container (`pgbouncer-userlist-init`) reads `db-user` and
`db-password` from the Secret and writes them to `/etc/pgbouncer/userlist.txt`
in the format PgBouncer expects:

```
"<username>" "<password>"
```

This keeps credentials out of the ConfigMap (which is not sensitive) while
ensuring PgBouncer can authenticate clients.

---

## Monitoring and observability

PgBouncer exposes statistics via its admin console (`psql -h 127.0.0.1 -p 5432
-U pgbouncer pgbouncer`). Useful queries:

```sql
-- Pool status
SHOW POOLS;

-- Per-database stats
SHOW STATS;

-- Active client and server connections
SHOW CLIENTS;
SHOW SERVERS;
```

To expose these metrics to Prometheus, deploy the
[pgbouncer_exporter](https://github.com/prometheus-community/pgbouncer_exporter)
as an additional sidecar or as a separate Deployment that connects to
PgBouncer's admin socket.

---

## Troubleshooting

### `prepared statement does not exist`

The application is sending named prepared statements to a transaction-pooling
PgBouncer. Fix: set `DB_STATEMENT_CACHE_MODE=describe`.

### `ERROR: no such user` in userlist.txt

The init container may have failed or the Secret keys may be wrong. Check:

```bash
kubectl logs <pod> -c pgbouncer-userlist-init
kubectl describe secret pgbouncer-credentials
```

### Connection refused on startup

PgBouncer starts asynchronously. The readinessProbe (`tcpSocket` on port 5432)
prevents traffic reaching the pod until PgBouncer is ready. If the application
container starts before PgBouncer, it will retry connections. The
`DB_POOL_CONNECT_TIMEOUT` and initial ping in `NewPool` will produce a clear
error message.

### Pool exhausted (`query_wait_timeout` hit)

Clients wait up to `query_wait_timeout` seconds before receiving an error.
Options:
- Increase `default_pool_size` (within Postgres `max_connections` budget).
- Reduce `max_client_conn` to surface back-pressure earlier.
- Investigate whether long-running transactions are holding backend connections.

### Idle-in-transaction connections aborted

PgBouncer aborts any transaction that stays idle longer than
`idle_transaction_timeout`. The Go pool will receive an error and the calling
code should handle it. If legitimate queries are being aborted:
- Increase `idle_transaction_timeout` (and `PGBOUNCER_IDLE_IN_TX_TIMEOUT`).
- Investigate whether application code is starting transactions and then doing
  unrelated work before committing.

---

## Security considerations

- PgBouncer binds to `127.0.0.1` only — no NetworkPolicy is needed to restrict
  inbound access within the pod.
- The egress NetworkPolicy (`networkpolicy-pgbouncer.yaml`) explicitly documents
  that the sidecar may egress to the Postgres backend.
- Credentials are stored in a Kubernetes Secret and written to a temporary
  `emptyDir` volume by the init container. They are never embedded in the
  ConfigMap.
- Use SCRAM-SHA-256 authentication (`auth_type = scram-sha-256`) if your
  Postgres version supports it (10+). It is significantly stronger than MD5.
- Enable backend TLS (`server_tls_sslmode = require`) to encrypt traffic
  between the sidecar and Postgres in production.
- The sidecar runs as UID 65534 (nonroot) with `allowPrivilegeEscalation=false`
  and all capabilities dropped.

See also: `deploy/kustomize/base/networkpolicy-pgbouncer.yaml` and
`deploy/helm/stellabill/templates/networkpolicy-pgbouncer.yaml`.
