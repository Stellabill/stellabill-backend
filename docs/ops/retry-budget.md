# Retry Budget

The retry budget prevents thundering-herd storms against downstream services by limiting retries to a configurable fraction of successful requests over a sliding time window.

## How it works

Every successful publish records a success in the budget. When a publish fails and a retry is needed, the budget checks whether the ratio of retries to successes is below the configured threshold. If the budget is exhausted, the retry is denied and the event fails fast — without compounding load on an already struggling downstream.

| Budget concept | Meaning |
|---------------|---------|
| Budget available | Fraction of unused retry capacity (1.0 = full, 0.0 = exhausted) |
| Retries denied | Spikes indicate downstream is unhealthy |

## Default tuning

| Parameter | Default | Description |
|-----------|---------|-------------|
| Ratio | 0.10 | Maximum allowed ratio of retries to successful requests |
| Window | 30 s | Sliding time window over which the ratio is computed |
| Buckets | 10 | Number of time buckets in the window (higher = smoother sliding) |

## Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `retry_budget_available` | Gauge | Fraction (0.0–1.0) of the retry budget still available. |
| `retry_denied_total` | Counter | Total number of retries denied because the budget was exhausted. |

## Dashboards and alerts

Configure a warning alert when `retry_budget_available` drops below 0.2 for more than one minute — this means 80 % of the retry budget has been consumed and downstreams are under pressure. A critical alert at 0.0 (budget exhausted) for more than 30 seconds should page the on-call engineer.

## See also

- [db-outage-runbook](./db-outage-runbook.md)
- [elevated-errors-runbook](./elevated-errors-runbook.md)
- [outbox-backlog](./runbooks/outbox-backlog.md)
