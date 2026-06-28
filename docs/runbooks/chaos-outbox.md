# Chaos Outbox Hook — Runbook

## Purpose

The `ChaosPublisher` decorator randomly cancels in-flight outbox publish
contexts when `ENV=staging`. This forces the dispatcher's retry and backoff
paths to be exercised continuously so that latent bugs surface before reaching
production.

## Activation

| Variable | Value | Effect |
|---|---|---|
| `ENV` | `staging` | Required — the hook is ignored in all other environments. |
| `CHAOS_OUTBOX_PROB` | `0.0`–`1.0` | Per-publish cancellation probability. |

**Default (disabled):** `CHAOS_OUTBOX_PROB=0`

## Tuning Guidance

Start conservatively and ratchet up:

| Probability | Behaviour |
|---|---|
| `0.01` (1 %) | ~1 cancellation per 100 publishes — mild retry exercise. |
| `0.05` (5 %) | ~5 cancellations per 100 — each goroutine sees a failure every few seconds at typical dispatch rates. |
| `0.10` (10 %) | Aggressive — expect visible retry backoff in log volume. |
| `> 0.25` | May cause sustained backlog if the outbox dispatcher batch size is large relative to throughput. |

## Observability

The counter `chaos_outbox_cancellations_total` increments on each injected
cancellation. Pair with:

- `outbox_publisher_lag_seconds` — watch for backlog growth.
- `rate(chaos_outbox_cancellations_total[5m])` — actual chaos rate.
- Dispatcher error logs — verify that retry/backoff runs correctly.

## Safety

- The hook is **entirely bypassed** when `ENV != staging`.
- A value of `0` (or unset) also bypasses — safe to deploy to staging with
  the env var absent.
- The returned error (`context.Canceled`) is treated as a transient publish
  failure, exactly like a network timeout. No events are lost; the dispatcher
  retries per its usual backoff schedule.
- The Prometheus counter is idempotent — double registration is caught by
  `prometheus.Register` at init time.
