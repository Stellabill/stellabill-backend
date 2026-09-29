# KEDA Worker Autoscaling from Outbox Backlog

**Service:** Stellabill worker (`stellabill-worker`)  
**Metric:** `outbox_backlog_depth{tenant}`  
**Manifest:** [`deploy/keda-scaledobject.yaml`](../../deploy/keda-scaledobject.yaml)  
**Related:** [`runbooks/outbox-backlog.md`](./runbooks/outbox-backlog.md)

---

## Purpose

Scale worker replicas from **1 → N** based on pending outbox depth so bursty
publish load drains quickly while idle clusters keep a single warm pod
(`minReplicaCount`).

The worker and outbox dispatcher refresh `outbox_backlog_depth` on each poll.
Tenant labels are length-capped (`64`) to bound Prometheus cardinality.
When the backlog is empty, a zero series (`tenant="none"`) is published so
KEDA still observes the metric and scales to `minReplicaCount`.

---

## Thresholds

| Knob | Value | Meaning |
|------|-------|---------|
| `minReplicaCount` | **1** | Idle / zero backlog floor |
| `maxReplicaCount` | **20** | Hard ceiling for burst scale-out |
| Prometheus `threshold` | **100** | Target pending events per replica (`DefaultBacklogPerReplica`) |
| `pollingInterval` | **15s** | How often KEDA re-evaluates |
| `cooldownPeriod` | **60s** | Delay before scale-down |
| Alert warning (ops) | **> 500** pending | See outbox backlog runbook |
| Alert critical (ops) | **> 2,500** pending | See outbox backlog runbook |

Conceptual replica target (mirrored in `outbox.DesiredWorkerReplicas`):

```text
if backlog <= 0:
    replicas = minReplicaCount          # always 1 when idle
else:
    replicas = clamp(ceil(backlog / 100), min, max)
```

Examples:

| Total `sum(outbox_backlog_depth)` | Desired replicas |
|-----------------------------------|------------------|
| 0 | 1 (`minReplicaCount`) |
| 50 | 1 |
| 101 | 2 |
| 500 | 5 |
| ≥ 2,000 | 20 (`maxReplicaCount`) |

---

## Apply

```bash
kubectl apply -f deploy/keda-scaledobject.yaml
kubectl get scaledobject -n stellabill
kubectl get hpa -n stellabill
```

Ensure Prometheus scrapes the worker (or API) `/metrics` endpoint and that
`serverAddress` in the ScaledObject points at a reachable Prometheus HTTP API.

---

## Verification

1. Confirm metric present: `curl -s localhost:8080/metrics | grep outbox_backlog_depth`
2. With empty backlog, gauge should be `0` and HPA desired replicas = `1`
3. Insert pending outbox rows (or load test) and confirm HPA desired rises toward `ceil(backlog/100)` capped at 20
4. Drain backlog and confirm cooldown then scale-down to 1

---

## Security notes

- Do not expose `/metrics` publicly; scrape only from the in-cluster Prometheus network.
- Tenant ids in labels are truncated; never attach PII-bearing labels beyond tenant id.
