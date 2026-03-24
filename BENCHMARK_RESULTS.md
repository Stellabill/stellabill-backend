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
