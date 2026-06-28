# Multi-Region Failover Playbook

**Service:** Stellabill Backend (Go/Gin + PostgreSQL)
**Owner:** On-call engineer → Backend team lead → Engineering manager
**Last updated:** 2026-05-12
**Related docs:** [`docs/ops/db-outage-runbook.md`](../ops/db-outage-runbook.md), [`docs/runbooks/capacity-planning.md`](./capacity-planning.md), [`docs/migrations.md`](../migrations.md)

---

## 1. Purpose & Scope

This playbook describes how to **promote a hot-standby replica** and **cut traffic over to a secondary region** for the Stellabill backend. It covers detection, the decision to fail over, the actual promotion sequence, traffic redirection, connection draining, verification, and rollback. It also defines the recurring drill schedule that exercises the procedure.

In scope:

- PostgreSQL primary → replica promotion in a second region.
- Routing the public API traffic to the promoted region.
- Draining open connections so in-flight work completes or fails cleanly.
- Verifying the new primary is healthy and the old region is isolated.

Out of scope:

- Cross-region replication topology for sub-second RPO (a separate runbook
  should cover stretch clusters and quorum commits; this playbook assumes
  the topology is already in place).
- Application-layer resharding (planned as a follow-up).

---

## 2. Targets (RTO / RPO)

| Metric | Target (warm-standby async) | Target (sync to hot-standby) | Validation evidence |
|---|---|---|---|
| **RPO** — data loss window | ≤ 60 s (replication lag at cutover) | **0** (committed → flushed → acknowledged before `200` returns) | Captured by `scripts/drills/failover.sh` in dry-run |
| **RTO** — time from decision to traffic on new primary | ≤ 15 min (manual) | ≤ 15 min (manual) | Measured across quarterly drills |
| **Detection → decision** | ≤ 5 min | ≤ 5 min | Alert + on-call rota |
| **Decision → fence (old region)** | ≤ 2 min | ≤ 2 min | `kubectl scale --replicas=0` + egress deny |
| **Fence → promotion** | ≤ 2 min | ≤ 2 min | `pg_ctl promote` step in drill |
| **Promotion → traffic cut** | ≤ 5 min | ≤ 5 min | Load-balancer / DNS flip |
| **Drain window** | 30 s grace period (matches `cmd/server/main.go:shutdownTimeout`) | 30 s | Hard-coded in current code |
| **Open connections at cutover** | All stale by ≤ 60 s | All stale by ≤ 60 s | Forced `db.Close()` + reconnect on API |

> **Notes:**
> - The **synchronous RPO is 0** by construction: PostgreSQL acknowledges the
>   commit to the client only after the WAL has been flushed to the synchronous
>   standby. Returning `0` here matches the database guarantee, not a SLA
>   emulator. **Async** RPO is bounded by the replication lag at cutover.
> - The 60 s async RPO assumes `wal_sender_timeout` and `max_wal_senders` are
>   tuned for the chosen replication mode (see §4 Prerequisites).
> - The 15 min RTO budget includes verification; promotion itself is well
>   under 30 s on a healthy replica.
> - Drain is currently governed by the hard-coded `shutdownTimeout = 30 s` in
>   `cmd/server/main.go`. We rely on the existing graceful-shutdown flow
>   (`internal/routes/routes.go` cleanup returns) rather than introducing a
>   dedicated knob yet — see §7.2.
> - Quarterly drills must validate both numbers — see §11.

---

## 3. Architecture (assumed topology)

```text
┌──────────────────────────────┐         async/sync          ┌──────────────────────────────┐
│  Primary Region              │   ───────────────────────▶  │  Secondary Region            │
│  (e.g. us-east-1)            │       WAL streaming          │  (e.g. us-west-2)            │
│                              │                             │                              │
│  • API pods (write + read)   │                             │  • API pods (read traffic)    │
│  • PostgreSQL primary        │                             │  • PostgreSQL hot-standby     │
│  • Read-replica DBTX router  │                             │  • Replica router (read)      │
└──────────────────────────────┘                             └──────────────────────────────┘
```

- The application uses `internal/db.ReadRouter` to route safe reads to the
  replica and writes to the primary. Routing is **transparent to handlers**.
- Promotion flips the role of the secondary: the replica becomes the
  primary, and the application no longer differentiates between regions.
- After promotion, the old primary must stay **read-only** until reconciled,
  or its traffic must be physically unreachable (preferred).

---

## 4. Prerequisites

Before a real failover can be executed, these must be true:

- [ ] **Replication is healthy.** `SELECT pg_last_wal_replay_lsn();` is within
      30 s of the primary's current WAL LSN (`pg_current_wal_lsn()`).
- [ ] **Standby is reachable.** `pg_is_in_recovery()` returns `true` on the
      standby and the connection from API pods succeeds.
- [ ] **`DATABASE_REPLICA_URL`** is set in the secondary region's deployment
      manifest; it falls back to the primary DSN if absent, but promotion
      requires a real replica DSN to be configured *after* promotion.
- [ ] **Load-balancer / DNS** has a tested failover toggle (e.g. Route 53
      record set with low TTL, or a load-balancer target group swap).
- [ ] **Graceful-shutdown budget is known.** `cmd/server/main.go` hard-codes
      `shutdownTimeout = 30 s`; the drain budget is bounded by it. Do not
      promise an RTO under this number without scope-creep on the codebase.
- [ ] **Quorum approval.** Promotion requires the on-call lead **and** a second
      engineer to acknowledge in the incident channel.
- [ ] **Credentials ready.** A short-lived admin token for the new region
      and a paused primary-region token to undo the cutover in case of rollback.

A missing prerequisite is **not** a reason to skip promotion; it is a reason to
**stop and resolve it** before ratifying the failover decision.

### 4.1 Replication-mode honesty

Pick the replication mode **before** the drill and stick with it during the
incident:

- **Synchronous replication (`synchronous_commit=on`, with
  `synchronous_standby_names` set):** the standby acknowledges the WAL
  flush before the client receives `200 OK`. RPO is 0 at the database
  level. **Choose this for any money-moving tenant.**
- **Asynchronous replication (`synchronous_commit=off` or `=local`):**
  the primary returns `200 OK` once the WAL is locally flushed, before
  the WAL reaches the standby. If the primary then fails *before* the
  WAL is streamed, the client receives `200 OK` for a write that is
  permanently lost on the cluster. Use this only for non-financial
  subsystems (logs, analytics, hints) and **never** as the default for
  subscriptions or payments.

The Idempotency-Key contract is the only client-side safety net for this
case; it does not eliminate the loss, it bounds duplicate-write risk on
retry. See §7.1 for the full breakdown.

---

## 5. Decision Criteria

Promote the secondary in **either** of these conditions on the primary region:

1. The primary's `/api/health` returns `"db": "down"` for **> 5 min** and
   restart attempts have failed.
2. The primary region is unreachable (network partition, zone-wide outage),
   confirmed by **two** independent paths (e.g. DB probes + load-balancer
   health checks + on-call's own VPN-less probe).

Do **not** promote for:

- Elevated latency alone (follow the [elevated-errors runbook](../ops/elevated-errors-runbook.md)).
- Slow queries or lock waits (follow the [DB outage runbook §7](../ops/db-outage-runbook.md#7-mitigation-steps)).
- Single-tenant API bugs (no need to shift an entire fleet).

The decision is logged in the incident channel before any state change takes
place.

---

## 6. Step-by-step Procedure

The phases are timed from the moment promotion begins. Numbers are budgeted
within the RTO target (§2).

### Phase 1 — Confirm & announce (T+0 → T+2 min)

- [ ] **1.1** Two engineers acknowledge the promotion in the incident
      channel ("PROCEED with promotion").
- [ ] **1.2** Post in `#incident`: "Promoting `<secondary-region>` as the
      new primary. RPO target ≤ 60 s (async) / 0 (sync)."
- [ ] **1.3** Page DBA team for cross-region replication sanity check.
- [ ] **1.4** Run `bash scripts/drills/failover.sh --dry-run --region=<secondary-region>`
      to confirm the drill script recognizes the secondary region and that
      the prerequisites (§4) are met. Dry-run must succeed.

### Phase 2 — Capture state for verification (T+2 → T+4 min)

- [ ] **2.1** Snapshot of the primary's last known good state:

  ```bash
  # Last WAL position before promotion (informational only — post-promotion
  # verification reads from the new primary).
  psql "$DATABASE_URL" -At -c "SELECT pg_current_wal_lsn();" \
    > /tmp/failover/primary-wal-before.lsn
  psql "$DATABASE_URL" -At -c "SELECT pg_is_in_recovery();" \
    > /tmp/failover/primary-recovery-before.lsn
  ```

- [ ] **2.2** In-flight request count (log query):

  ```bash
  journalctl -u stellabill-backend --since "1 minute ago" --no-pager -o json \
    | jq -r 'select(.message | test("request completed"))' | wc -l
  ```

- [ ] **2.3** Add a maintenance banner so clients can see the brief outage.
      The codebase currently surfaces errors via `ErrorEnvelope` (see
      `docs/ERROR_ENVELOPE.md`); no feature-flag plumbing exists yet —
      communicate the window in the incident channel and the public status
      page.

### Phase 3 — Fence the old primary (T+4 → T+6 min) — split-brain guard

> **Critical.** This phase fences off the old region **before** the new
> primary takes writes. If the old primary recovers during Phase 4, any
> client that still points at it must hit a network fence rather than
> succeeding.

- [ ] **3.1** Scale the old-region API deployment to zero:

  ```bash
  kubectl scale deployment/stellabill-backend --replicas=0 \
    --context="$OLD_REGION_CONTEXT"
  ```

- [ ] **3.2** Deny egress from the old-region pods to the database (network
      policy; or revoke the IAM role that allows the connection):

  ```yaml
  apiVersion: networking.k8s.io/v1
  kind: NetworkPolicy
  metadata:
    name: deny-old-region-egress
    namespace: stellabill
  spec:
    podSelector: { matchLabels: { app: stellabill-backend } }
    policyTypes: [Egress]
    egress:
      - to:
          - ipBlock:
              cidr: 0.0.0.0/0
              except:
                - $OLD_DB_CIDR/32
  ```

- [ ] **3.3** Confirm fences:

  ```bash
  kubectl get networkpolicy deny-old-region-egress --context="$OLD_REGION_CONTEXT"
  # Confirm 0 replicas:
  kubectl get deploy stellabill-backend --context="$OLD_REGION_CONTEXT" \
    -o jsonpath='{.spec.replicas}'
  ```

  Once both succeed, **no client of the old region can register a write**.
  Split-brain risk is now bounded by fencing rather than routing order.

### Phase 4 — Promote standby (T+6 → T+8 min)

Run **on the standby host** (or via your provider's RDS managed promotion):

```bash
# 1. Confirm replica is still healthy before promoting
psql "$DATABASE_REPLICA_URL" -At -c "SELECT pg_is_in_recovery();"
# Expect: t

# 2. Promote the standby to primary.  On RDS this is "Failover master" via
#    the provider console; on self-managed Postgres:
ssh standby "pg_ctl promote -D /var/lib/postgresql/data"

# 3. Verify the new primary is writable
ssh standby psql "$DATABASE_REPLICA_URL" -At -c "SELECT pg_is_in_recovery();"
# Expect: f
ssh standby psql "$DATABASE_REPLICA_URL" -At -c "CREATE TABLE _failover_probe(id int); DROP TABLE _failover_probe;"
```

If promotion fails, **stop**. Do not proceed. Restore fences/gates and
follow Phase 7.

### Phase 5 — Route API traffic (T+8 → T+11 min)

- [ ] **5.1** Drain in-flight requests from the **old primary region's
      API**. Because of Phase 3's scale-to-zero, the old region has no
      healthy endpoints; the goal here is to wait for the 30 s
      `shutdownTimeout` in `cmd/server/main.go` to let in-flight requests
      finish, then close the DB pool (see `internal/routes/routes.go`
      cleanup path). Total drain budget: **30 s** plus DB pool close seconds,
      expected to be ≤ 60 s end-to-end.

- [ ] **5.2** Redirect traffic at the edge:

  ```bash
  # Route 53 example (replace $NEW_REGION_ALB_DNS with the new region's ALB):
  aws route53 change-resource-record-sets \
    --hosted-zone-id "$HOSTED_ZONE" \
    --change-batch file:///tmp/failover/route53-failover.json

  # Or on a managed Kubernetes ingress:
  kubectl patch ingress stellabill \
    --patch "$(cat /tmp/failover/new-ingress.yaml)" \
    --context="$NEW_REGION_CONTEXT"
  ```

  Low TTL (≤ 60 s) on the public DNS record is required for this budget to
  hold. Production records should already be at 60 s TTL.

- [ ] **5.3** The **secondary region's API pods** were already pulling from
      the replica DSN. Once promotion completes (`internal/db/router.go`
      `Reader()`'s routing based on freshness token), writes via
      `ExecContext`/`PrepareContext` go to the **OLD primary** DSN
      (`cfg.DBConn`) until you rotate secrets in step 5.4 — that is why
      Phase 3's fencing is essential.

- [ ] **5.4** Update secrets in the **secondary region** so any subsequent
      pod rollout points `cfg.DBConn` at the promoted replica DSN. Restart
      the API pods to pick up the new DSN:

  ```bash
  kubectl rollout restart deployment/stellabill-backend \
    --context="$NEW_REGION_CONTEXT"
  kubectl rollout status deployment/stellabill-backend --context="$NEW_REGION_CONTEXT"
  ```

### Phase 6 — Verify (T+11 → T+14 min)

- [ ] **6.1** Health probe from outside the region:

  ```bash
  curl -sf https://api.stellarbill.example.com/api/health | jq .
  # Expect: { "service": "stellarbill-backend", "status": "ok", "db": "up", "worker": "running" }
  ```

- [ ] **6.2** End-to-end write smoke test (use a low-privilege staging
      tenant credential; never touch production data):

  ```bash
  curl -sf -X POST https://api.stellarbill.example.com/api/subscriptions \
       -H "Authorization: Bearer $STAGING_TOKEN" \
       -H "X-Tenant-ID: staging-tenant" \
       -H "Content-Type: application/json" \
       -d '{"plan_id":"failover-probe"}' | jq .
  ```

- [ ] **6.3** Replication lag check from the **old primary** if reachable:

  ```bash
  psql "$DATABASE_URL" -c "SELECT pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn();"
  # If the old primary recovered as a replica of the new primary, the lag
  # should be sub-second; if it's still down, lag is N/A.
  ```

- [ ] **6.4** Outbox dispatch — verify `[ "outbox" ]["dispatcher_running" ]`
      is `true` in step 6.1. If not, restart the dispatcher in the new region.

- [ ] **6.5** Announce "RTO achieved at <HH:MM:SS>" in the incident channel.

### Phase 7 — Rollback (if anything fails)

If the promotion succeeded but traffic verification fails (Phase 6):

- [ ] **7.1** Re-flip the edge load-balancer / DNS back to the original
      region and **remove** the fences from Phase 3.
- [ ] **7.2** If the original region is unreachable, leave traffic on the new
      region. Do **not** demote; the rollback is a *traffic* rollback only.
- [ ] **7.3** If promotion itself failed (Phase 4 errored), remove fences,
      abort the procedure, and follow the
      [DB outage runbook](../ops/db-outage-runbook.md) on the original region.

Demoting a primary back to a standby is much harder than not promoting it. The
default action for any anomaly during promotion is **stop and call DBA**.

---

## 7. Edge Cases

### 7.1 Promotion mid-write

**Scenario:** A write transaction is in flight on the old primary when
`pg_ctl promote` runs.

#### What PostgreSQL guarantees

- **Sync mode (`synchronous_commit=on` with synchronous standby):** the
  commit is acknowledged to the client only after the WAL has been streamed
  to the standby and replicated. Any `200 OK` the client received is durable
  on the new primary after promotion. **RPO = 0.**
- **Async mode (`synchronous_commit=off`):** the commit is acknowledged to
  the client once WAL is locally flushed, before streaming reaches the
  standby. If the primary dies *between* local flush and streaming, the
  client received `200 OK` for a write that **never reaches the new
  primary**. This is a silent data loss from the client's perspective and a
  real contribution to RPO.

#### What the application does

- The application uses `BeginTx` (see `internal/db/dbtx.go`); uncommitted
  transactions return `400`/`500` to the caller, and Gin request logs make
  them visible at the same time as the promotion event.
- For successful (`200 OK`) writes under async replication, the application
  has **no way to know** the write was lost until reconciliation. The
  Idempotency-Key contract (see [`docs/idempotency.md`](../idempotency.md))
  only prevents *duplicates* on retry; it cannot resurrect a lost write.
- The outbox pattern buffers events in the same database, so any lost write
  also loses its outbound event.

#### Recommended handling

- **Pick sync mode for any write path that touches money**, even at the
  cost of higher latency under primary load. This is the only way to make
  `200 OK` mean "durable, replicated, will survive failover".
- **Correlate with payment gateways.** For writes that have a downstream
  side-effect on an external system (Stripe, etc.), reconcile from the
  gateway's records to recover from silent data loss. This is a backstop,
  not a primary defence.
- **Drill it.** `scripts/drills/failover.sh --case mid-write` injects a
  write 250 ms before promotion and asserts the response code and
  replication outcome. The drill **fails** if async mode loses a
  `200 OK`-acknowledged write that the client believed succeeded.

**RPO contribution under async:** a `200 OK`-acknowledged write that is not
yet streamed is permanently lost on the cluster. The next reconciliation run
detects the mismatch. The Idempotency-Key contract prevents double-spend on
retry but does not prevent the initial loss.

**RPO contribution under sync:** 0. Any `200 OK` is durable.

### 7.2 Stuck connection draining

**Scenario:** Long-running queries hold connections open past the drain
window, leaving the API pods unable to release the listener cleanly.

#### Current behaviour (honest)

- `cmd/server/main.go` hard-codes `shutdownTimeout = 30 * time.Second`. The
  HTTP shutdown gives in-flight requests 30 s to complete.
- After that, `routes.RegisterWithCleanup` runs:
  - `dbPool.Close()` — `pgxpool.Pool.Close()` waits for active queries to
    finish.
  - `planDB.Close()` and `replicaDB.Close()` — `sql.DB.Close()` waits for
    the underlying connections; it does **not** kill long-running queries
    by itself.
- The shutdown context passed to `cleanup(shutdownCtx)` is, in practice, no
  longer bound by the original 30 s budget; `db.Close()` calls can in
  principle block beyond the budget. This is a known issue tracked
  separately; do not assume tight bounded drain in the worst case.

#### Handling the drill

- The drill (`--case stuck-connection`) starts a query that intentionally
  exceeds the 30 s `shutdownTimeout`.
- It then issues a `kill -TERM` to the API process and asserts:
  - The query completes (or is cancelled by Postgres on connection close).
  - No connection remains in `pg_stat_activity` from the API DSN after
    60 s.
- Worst case: a query that exceeds `READ_TIMEOUT`/`WRITE_TIMEOUT` (30 s) is
  killed with `5xx` to the client. Unexpected blocking on `db.Close()` is
  reported and filed as a follow-up.

#### Future change (tracked, not yet implemented)

The right fix is to bound the `db.Close()` calls behind a derived context
(`context.WithTimeout(parent, drainTimeout)`) so cleanup never blocks past
the budget. That change is tracked separately and **does not** block
quarterly drills, but the drill documents the gap so reviewers know.

### 7.3 Old primary recovers as replica

Sometimes the old primary comes back online after promotion. The safe way
to re-attach it is as a **new replica** of the promoted primary, not as a
primary. The drill verifies:

- The old primary's DSN in the **new primary region's** deployment manifest
  is removed.
- The old primary is reachable only via a one-off emergency endpoint
  (`/api/diagnostics`) for read-only inspection — never for writes.

### 7.4 Outbox events during the cutover

The outbox dispatcher publishes from the database; during the cutover
disconnected events will sit in the `outbox` table until the new primary's
dispatcher picks them up. This is safe: outbox guarantees at-least-once
delivery. The drill asserts:

- `outbox.dispatcher_running == true` after cutover.
- Pending unprocessed events drain within `3 * DRAIN_SECONDS`.

---

## 8. Verification Checklist

After RTO is declared, mark each item:

- [ ] `/api/health` returns `db: up, worker: running` for **5 consecutive
      minutes** in the new region.
- [ ] End-to-end write smoke test succeeds (Phase 5.2).
- [ ] Outbox dispatcher is running in the new region.
- [ ] Old-region API pods are scaled to zero replicas.
- [ ] DNS / load-balancer is healthy in the new region (no `5xx` edge errors
      in the past 5 min).
- [ ] Customer-facing status page updated.
- [ ] Incident ticket links to this playbook + the post-drill report.

---

## 9. Security Assumptions

- The promoted replica's `DATABASE_REPLICA_URL` is stored in the secrets
  manager and is **rotated** for the new primary role. The old DSN is
  revoked (or, if reusable, restricted to the read-only diagnostics
  endpoint).
- TLS for database connections is enforced in production (`sslmode=verify-full`
  is the default in self-managed and in RDS) — the drill asserts this
  flag is `verify-full` or `require`.
- Promotion events themselves are **not** auto-logged by the Go audit
  middleware: `pg_ctl promote` (or the cloud-provider failover API) runs
  out-of-band of the application. The record of the action lives in:
  - The cloud-provider audit trail (CloudTrail for RDS, Activity Log for
    Azure DB, Cloud Audit Log for Cloud SQL).
  - PostgreSQL's own server logs (`pg_log`).
  - The incident channel transcript.
  - The audit middleware logs the *subsequent* API-level events (writes,
    admin actions) on the new primary, but not the promotion itself.
- No credentials are written to the playbook's command-line examples.
  Production commands must use environment variables populated from the
  secrets manager.

### 9.1 Where the playbook should — and shouldn't — match the code

- Use the read-replica router (`internal/db/router.go`) so reads continue
  on the new primary without code changes.
- Use `BeginTx` from `internal/db/dbtx.go` as the canonical transaction
  boundary for any mid-write analysis.
- Rely on the existing graceful-shutdown path (`cmd/server/main.go`,
  `internal/routes/routes.go`) for drain, **but** document the known
  hard-coded 30 s and unbounded `db.Close()` behaviour rather than
  pretending a configurable knob exists.
- Do **not** claim that `INTERNAL_DRAIN_SECONDS` or `FAILOVER_DRAIN_SECONDS`
  is honoured by the codebase until it is implemented (see §7.2).
- Quarterly drills run in **staging only**, with read-only credentials
  that cannot touch production data. The drill script refuses to run
  when `ENV=production` unless `--confirm-prod` is set.

---

## 10. Quarterly Drill Schedule

| Quarter | Window | Owner | Drill type | Notes |
|---|---|---|---|---|
| Q1 (Jan–Mar) | 2nd Tuesday, 14:00 UTC | On-call rotation | **Surprise** tabletop + 30-min chaos cutover | Acts as a real failover test on a synthetic dataset |
| Q2 (Apr–Jun) | 2nd Tuesday, 14:00 UTC | Backend lead | **Scheduled** mid-write drill (`--case mid-write`) | Validates `BeginTx` behaviour during promotion |
| Q3 (Jul–Sep) | 2nd Tuesday, 14:00 UTC | SRE rotation | **Scheduled** stuck-connection drill (`--case stuck-connection`) | Validates `db.Close()` after drain window |
| Q4 (Oct–Dec) | 2nd Tuesday, 14:00 UTC | Engineering manager | **Surprise** end-to-end (table-top + drill) | Year-end readiness |

### Drill outputs

Each drill produces a report containing:

- The full `bash scripts/drills/failover.sh ...` invocation log.
- Measured RPO and RTO from the drill instrumentation.
- Replication lag at cutover (`pg_last_wal_replay_lsn()` delta).
- Discrepancies vs. the §2 targets (any overshoot is an action item).

Reports are committed to `docs/runbooks/drill-reports/YYYY-QN.md` and reviewed
in the next all-hands.

### Post-drill checks

- [ ] Drill report committed and rotated into `docs/runbooks/drill-reports/`.
- [ ] Drift from §2 targets documented.
- [ ] Any new edge case encountered is added to §7 and gated by a test case
      in `scripts/drills/failover.sh`.
- [ ] Quarterly drill reminder banner reconfirmed in `README.md`
      (see `## Quarterly drills reminder` section).

---

## 11. Quick Reference Card

| Step | Time | Action | Tool |
|---|---:|---|---|
| 1 | T+0 | Announce + dry-run | `bash scripts/drills/failover.sh --dry-run --region=<secondary>` |
| 2 | T+2 | Snapshot primary WAL | `psql … pg_current_wal_lsn()` |
| 3 | T+4 | Fence old region (split-brain guard) | `kubectl scale --replicas=0` + `NetworkPolicy` |
| 4 | T+6 | Promote standby | `pg_ctl promote` / RDS failover |
| 5 | T+8 | Drain + flip LB | `kubectl rollout restart` + `route53` |
| 6 | T+11 | Verify health + smoke test | `curl /api/health`, `POST /api/subscriptions` |
| 7 | T+13 | Roll back (remove fences first) | route53 revert + `kubectl delete networkpolicy` |
| 8 | T+14 | Declare RTO achieved | incident channel |

Print this card during the on-call shift handover.
