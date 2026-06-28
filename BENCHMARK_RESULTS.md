# Benchmark Results

## Overview

Performance benchmarks for plans and subscriptions list endpoints.

## Running Benchmarks

```bash
# Quick run
go test ./internal/handlers/... -bench=. -benchmem

# Full suite with scripts
./scripts/run_benchmarks.sh

# Compare with baseline
./scripts/analyze_benchmarks.sh baseline.txt new.txt
```

## Load Test Smoke Profile

The repository includes a k6 smoke profile that exercises the API with real HTTP requests and authorization. It validates end-to-end service behavior under load and enforces:

- p95 latency < `250ms`
- error rate < `0.1%`

```bash
make loadtest-smoke
```

For remote targets, set `LOADTEST_TARGET` and `JWT_SECRET`:

```bash
LOADTEST_TARGET=https://staging.example.com \
JWT_SECRET=${JWT_SECRET} \
make loadtest-smoke
```

## Benchmark Categories

### 1. Dataset Size Tests
- Empty, Small (10), Medium (100), Large (1K), XLarge (10K)

### 2. JSON Encoding Tests
- Isolates serialization performance

### 3. Full HTTP Tests
- Complete request/response cycle

### 4. Parallel Tests
- Concurrent request handling

### 5. Filtered Tests
- Query parameter filtering

## Expected Performance

See BENCHMARK_GUIDE.md for detailed baselines and thresholds.

## CI Integration

Benchmarks run automatically on PRs to detect regressions.

## Analysis

Use benchstat for statistical comparison:

```bash
go install golang.org/x/perf/cmd/benchstat@latest
benchstat baseline.txt new.txt
```
