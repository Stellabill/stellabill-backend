# Argo Rollouts — Canary Strategy

This document describes the canary deployment strategy for the `stellabill-api`
component using [Argo Rollouts](https://argoproj.github.io/argo-rollouts/).

---

## Overview

Instead of a rolling Deployment, the API uses an Argo Rollouts `Rollout` resource
that shifts traffic progressively: **10% → 25% → 50% → 100%**.

Between each step, automated analysis queries Prometheus for:

| Metric | Threshold | Failure action |
|---|---|---|
| p99 request latency | < 500 ms (configurable) | Abort + rollback |
| HTTP 5xx error rate | < 1% of requests (configurable) | Abort + rollback |

If analysis fails at any step, Argo Rollouts automatically aborts and returns
all traffic to the previous stable revision.

---

## Prerequisites

**Argo Rollouts controller** must be installed in the cluster:

```bash
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts \
  -f https://github.com/argoproj/argo-rollouts/releases/latest/download/install.yaml

# Install the kubectl plugin (optional but recommended)
curl -LO https://github.com/argoproj/argo-rollouts/releases/latest/download/kubectl-argo-rollouts-linux-amd64
chmod +x kubectl-argo-rollouts-linux-amd64
mv kubectl-argo-rollouts-linux-amd64 /usr/local/bin/kubectl-argo-rollouts
```

**Prometheus** must be reachable at `rollout.analysis.prometheusAddress`
(default: `http://prometheus-operated.monitoring.svc.cluster.local:9090`).

---

## Enabling the canary strategy

Set `rollout.enabled=true` when installing or upgrading the chart:

```bash
helm upgrade stellabill deploy/helm/stellabill \
  --set rollout.enabled=true \
  --set image.tag=v1.2.3 \
  --namespace stellabill
```

When `rollout.enabled=false` (default), the chart deploys a standard `Deployment`.

---

## Helm values reference

```yaml
rollout:
  enabled: false                        # flip to true to activate
  revisionHistoryLimit: 3

  canary:
    step1PauseDuration: "2m"            # 10% → pause → 25%
    step2PauseDuration: "2m"            # 25% → pause → 50%
    step3PauseDuration: "5m"            # 50% → pause → 100%
    abortScaleDownDelaySeconds: 30
    scaleDownDelaySeconds: 30

  analysis:
    templateName: stellabill-canary-analysis
    prometheusAddress: http://prometheus-operated.monitoring.svc.cluster.local:9090
    intervalSeconds: 60                 # query Prometheus every 60s
    count: 5                            # 5 measurements per metric
    failureLimit: 1                     # 1 failure triggers abort
    inconclusiveLimit: 3                # 3 zero-sample results tolerated
    p99LatencyThresholdMs: 500          # p99 must stay below 500ms
    errorRateThreshold: 0.01            # 5xx rate must stay below 1%
```

---

## Monitoring a rollout

```bash
# Watch rollout progress in real time
kubectl argo rollouts get rollout stellabill-api -n stellabill --watch

# Short status
kubectl argo rollouts status stellabill-api -n stellabill
```

Example output during canary:

```
Name:            stellabill-api
Namespace:       stellabill
Status:          ॥ Paused
Message:         CanaryPauseStep
Strategy:        Canary
  Step:          2/8
  SetWeight:     10
  ActualWeight:  10
Images:          stellabill/backend:v1.2.2 (stable)
                 stellabill/backend:v1.2.3 (canary)
```

---

## Manual operations

### Promote (skip remaining pause and advance to next step)

```bash
kubectl argo rollouts promote stellabill-api -n stellabill
```

### Full promote (skip all remaining steps and go straight to 100%)

```bash
kubectl argo rollouts promote stellabill-api -n stellabill --full
```

### Abort (stop the canary and roll back to stable)

```bash
kubectl argo rollouts abort stellabill-api -n stellabill
```

### Undo (roll back to the previous revision)

```bash
kubectl argo rollouts undo stellabill-api -n stellabill
```

### Retry after abort

```bash
kubectl argo rollouts retry rollout stellabill-api -n stellabill
```

---

## Analysis edge cases

| Scenario | Behaviour |
|---|---|
| Zero samples (no traffic yet) | `Inconclusive` — rollout pauses, retries up to `inconclusiveLimit` times |
| p99 latency spike above threshold | `Failed` → automatic abort + rollback |
| 5xx error rate above threshold | `Failed` → automatic abort + rollback |
| Prometheus unreachable | `Error` → treated as failure after `failureLimit` |
| Analysis succeeds all steps | Rollout completes, canary becomes new stable |

---

## Rollback runbook

If a rollout is aborted automatically:

1. **Check analysis results:**
   ```bash
   kubectl argo rollouts get rollout stellabill-api -n stellabill
   # Look for AnalysisRun resources listed under the rollout
   kubectl get analysisrun -n stellabill
   kubectl describe analysisrun <name> -n stellabill
   ```

2. **Check Prometheus metrics** to understand the failure signal.

3. **Fix the issue** in the new image or configuration.

4. **Build and push** a corrected image tag.

5. **Update the rollout:**
   ```bash
   helm upgrade stellabill deploy/helm/stellabill \
     --set rollout.enabled=true \
     --set image.tag=v1.2.4 \
     --namespace stellabill
   ```

---

## Disabling canary (revert to Deployment)

```bash
helm upgrade stellabill deploy/helm/stellabill \
  --set rollout.enabled=false \
  --namespace stellabill
```

This re-creates the standard `Deployment` resource. The `Rollout` resource
is not automatically deleted — remove it manually if needed:

```bash
kubectl delete rollout stellabill-api -n stellabill
kubectl delete analysistemplate stellabill-canary-analysis -n stellabill
```

---

## Security notes

- The `AnalysisTemplate` queries Prometheus read-only; no credentials are
  stored in the template — configure Prometheus RBAC separately if required.
- The `Rollout` serviceAccountName follows the same convention as the
  `Deployment` (`<release>-api`) — no additional RBAC is needed.
- Analysis queries are scoped by `service` and `namespace` labels to prevent
  cross-tenant metric pollution.
