# Capacity Planning Playbook

This playbook turns production Prometheus snapshots into a repeatable sizing model for Stellabill. The goal is to estimate the cost of one tenant and then map tenant counts to required app and database capacity.

## What to Measure

Capture two snapshots from the same production environment:

- A baseline snapshot under low or zero tenant activity.
- A peak snapshot after a representative load window.

Required metrics:

- `process_cpu_seconds_total`
- `process_resident_memory_bytes`
- `db_pool_stats{stat="active_conns"}`
- `db_pool_stats{stat="max_conns"}`
- `db_queries_total`
- `pg_stat_database_blks_read`
- `pg_stat_database_blks_written`

If the Postgres exporter exposes additional I/O metrics, include them in the same snapshot. Do not include request bodies, secrets, or token values in the export.

## Reproducible Collection Script

Use the helper script to capture the baseline and peak snapshots:

```bash
scripts/capacity-collect.sh http://localhost:8080/api/metrics 300 ./artifacts/capacity
```

The script writes:

- `baseline-*.prom`
- `peak-*.prom`
- `metadata-*.json`

For a real production run, point the URL at the read-only metrics endpoint and keep the collection window short enough to avoid noisy traffic changes.

## Sizing Model

The sizing tool computes a linear per-tenant estimate from the baseline and peak snapshots:

- `cpu_mcores_per_tenant = ((peak_cpu_seconds_total - base_cpu_seconds_total) / window_seconds) * 1000 / observed_tenants`
- `memory_mib_per_tenant = (peak_memory_bytes - base_memory_bytes) / MiB / observed_tenants`
- `postgres_iops_per_tenant = ((peak_blks_read + peak_blks_written) - (base_blks_read + base_blks_written)) / window_seconds / observed_tenants`
- `db_queries_qps_per_tenant = (peak_db_queries_total - base_db_queries_total) / window_seconds / observed_tenants`

The recommended cluster size is then:

- `required_cpu = headroom * cpu_mcores_per_tenant * target_tenants`
- `required_memory = headroom * (baseline_memory_mib + memory_mib_per_tenant * target_tenants)`
- `required_postgres_iops = headroom * postgres_iops_per_tenant * target_tenants`

The default headroom is `1.25`.

## Tooling

Run the planner against the captured snapshots:

```bash
go run ./tools/capacity \
  -base ./artifacts/capacity/baseline.prom \
  -peak ./artifacts/capacity/peak.prom \
  -window 5m \
  -tenants 120 \
  -tenant-counts 50,100,250
```

Optional output:

```bash
go run ./tools/capacity -base ... -peak ... -window 5m -tenants 120 -json
```

## Alerting Thresholds

Use these thresholds as the initial alert baseline, then tune them against your own production histograms:

| Signal | Warning | Critical |
|---|---:|---:|
| App CPU | > 70 % of pod request for 10 min | > 85 % for 5 min |
| App memory | > 75 % of pod limit for 10 min | > 90 % for 5 min |
| DB pool saturation | active connections > 80 % of max | > 90 % or sustained acquire failures |
| DB query latency | p99 > 500 ms | p99 > 2 s |
| Postgres IOPS | > 70 % of provisioned budget | > 90 % of provisioned budget |

For app-level DB pool saturation, use `db_pool_stats{stat="active_conns"}` and `db_pool_stats{stat="max_conns"}`. For Postgres saturation, rely on your Postgres exporter or cloud provider metrics.

## Tenant Profiles

Validate two edge cases before promoting sizing guidance:

1. Zero-traffic tenant profile
   - Baseline and peak snapshots are effectively identical.
   - Per-tenant CPU and IOPS should evaluate to zero.
   - Memory should still preserve the baseline process footprint.
2. Burst-traffic tenant profile
   - The peak snapshot should include a clear traffic spike.
   - CPU, query rate, and IOPS should all increase monotonically.
   - The resulting replica count should not decrease as tenant counts rise.

## Security Notes

- Use read-only metrics access.
- Do not query production databases with write permissions for sizing.
- Do not export PII, tokens, request bodies, or authorization headers.
- Keep the metrics snapshots and generated reports in a restricted artifact location.

## Review Checklist

- [ ] Baseline and peak snapshots are archived.
- [ ] Tenant count used for the measurement window is documented.
- [ ] `go run ./tools/capacity ...` output is attached to the change.
- [ ] Alert thresholds were validated against production history.
- [ ] No sensitive values appear in the snapshot files or report.
