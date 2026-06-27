# Benchmark Regression Gate

## Purpose

Every pull request that targets `main` is automatically checked for Go benchmark
performance regressions.  If **any** tracked benchmark regresses by more than
**10 %** compared to the `main` baseline, CI fails and the PR cannot be merged.

---

## How it works

```
PR opened / updated
       │
       ▼
┌─────────────────────────────────────────────────────────────┐
│  benchmark-regression-gate.yml  (PR gate job)               │
│                                                             │
│  1. go test -bench=. -count=10 ./internal/handlers/...      │
│     on the PR head  →  /tmp/bench_head.txt                  │
│                                                             │
│  2. Download benchmark-baseline-main artifact               │
│     (uploaded to GitHub Actions by the baseline job)        │
│     →  /tmp/baseline/bench_baseline.txt                     │
│                                                             │
│  3. benchstat baseline head  →  /tmp/benchstat_output.txt   │
│                                                             │
│  4. Parse output; fail if any +delta > 10 %                 │
└─────────────────────────────────────────────────────────────┘

main push / merge
       │
       ▼
┌──────────────────────────────────────────────────────────┐
│  update-benchmark-baseline job                           │
│                                                          │
│  1. go test -bench=. -count=10 ./internal/handlers/...  │
│  2. Upload bench_baseline.txt as benchmark-baseline-main │
│     (retention: 90 days, overwrite: true)               │
└──────────────────────────────────────────────────────────┘
```

### Why `count=10`?

`benchstat` needs multiple samples to compute a confidence interval.  Ten
samples is sufficient for narrow CI bands while keeping total CI time under
five minutes for the current suite.

### Why `ubuntu-22.04` (pinned)?

`ubuntu-latest` changes over time.  Hardware differences between image
generations can shift benchmark results by several percent, which would create
false positives.  Pinning to `ubuntu-22.04` keeps the runner class stable.

---

## Files

| Path | Purpose |
|------|---------|
| `.github/workflows/benchmark-regression-gate.yml` | CI workflow (gate + baseline updater) |
| `scripts/check_benchmark_regression.sh` | Parser used by the workflow; also usable locally |
| `scripts/check_benchmark_regression_test.sh` | Bash unit tests for the parser |
| `docs/BENCHMARK_REGRESSION_GATE.md` | This document |

---

## Running locally

### Quick smoke test (1 iteration, fast)

```bash
go test -bench=. -benchtime=1x -run=^$ ./internal/handlers/...
```

### Full comparison (matches CI)

```bash
# Record main baseline
git checkout main
go test -bench=. -count=10 -run=^$ ./internal/handlers/... | tee /tmp/base.txt

# Record your branch
git checkout my-branch
go test -bench=. -count=10 -run=^$ ./internal/handlers/... | tee /tmp/head.txt

# Compare
benchstat /tmp/base.txt /tmp/head.txt

# Or use the script (mirrors CI logic exactly)
benchstat /tmp/base.txt /tmp/head.txt | bash scripts/check_benchmark_regression.sh 10
```

Install `benchstat` if needed:

```bash
go install golang.org/x/perf/cmd/benchstat@latest
```

---

## Edge cases

### First run – no baseline exists

On the very first PR after the workflow is added, no `benchmark-baseline-main`
artifact exists yet.  The gate detects this and **skips the comparison**,
printing an informational message in the step summary.  The gate does not fail.

After the PR merges, `update-benchmark-baseline` runs on `main` and creates the
artifact for all future PRs.

### New benchmark added in a PR

`benchstat` marks new benchmarks as `(new)` with no delta column.  The parser
skips lines without a positive delta, so new benchmarks never trigger a failure.

### Benchmark removed from a PR

`benchstat` marks removed benchmarks as `(gone)`.  These also have no positive
delta and are skipped.

### Flaky statistical noise

`benchstat` computes a p-value across the 10 samples.  Lines where the change
is not statistically significant are marked with `~` and no percentage, so they
are not parsed at all.  The gate only acts on **statistically significant
regressions** that also exceed the magnitude threshold.

### Artifact expiry (90-day window)

Baseline artifacts are retained for 90 days.  If a baseline expires (e.g., a
feature branch dormant for >90 days), the gate will skip the comparison and
succeed on the first run, then record a new baseline after merge.

---

## Adjusting the threshold

The threshold is controlled by the workflow env var:

```yaml
env:
  REGRESSION_THRESHOLD_PERCENT: "10"
```

Change it to `15` for a looser gate or `5` for a tighter one.  The script
accepts it as its first argument, so local usage stays in sync automatically.

---

## Running the parser tests

```bash
bash scripts/check_benchmark_regression_test.sh
```

Expected output: `9 passed, 0 failed`.