# Handler Benchmarks

## Overview

Comprehensive benchmark suite for plans and subscriptions list endpoints to establish performance baselines and detect regressions.

## Quick Start

```bash
# Run all benchmarks
go test ./internal/handlers/... -bench=. -benchmem

# Run specific endpoint
go test ./internal/handlers/... -bench=BenchmarkListPlans -benchmem

# Use helper script
./scripts/run_benchmarks.sh
```

## Benchmark Structure

### Files

- `plans_benchmark_test.go` - Plans endpoint benchmarks
- `subscriptions_benchmark_test.go` - Subscriptions endpoint benchmarks
- `benchmark_test.go` - Comparison and analysis benchmarks
- `fixtures_test.go` - Fixture generation tests
- `benchmark_thresholds.go` - Performance thresholds

### Categories

1. **Dataset Size**: Empty, Small (10), Medium (100), Large (1K), XLarge (10K)
2. **JSON Encoding**: Isolated serialization performance
3. **Full HTTP**: Complete request/response cycle
4. **Parallel**: Concurrent request handling
5. **Filtered**: Query parameter filtering

## Fixtures

Realistic test data generated with:

- **Plans**: ID, name, amount, currency, interval, description
- **Subscriptions**: ID, plan_id, customer, status, amount, interval, next_billing

Fixtures include varied data distributions to simulate real-world scenarios.

## Performance Thresholds

Defined in `benchmark_thresholds.go`:

```go
ThresholdPlansSmall = BenchmarkThresholds{
    MaxLatencyNs: 30000,   // 30 µs
    MaxAllocsOp:  25,
    MaxBytesOp:   15000,
}
```

## Running Benchmarks

### Basic

```bash
go test -bench=. -benchmem
```

### With Profiling

```bash
# CPU profile
go test -bench=. -cpuprofile=cpu.prof
go tool pprof cpu.prof

# Memory profile
go test -bench=. -memprofile=mem.prof
go tool pprof mem.prof
```

### Comparison

```bash
# Baseline
go test -bench=. -benchmem > baseline.txt

# After changes
go test -bench=. -benchmem > new.txt

# Compare
benchstat baseline.txt new.txt
```

## CI Integration

Benchmarks run automatically on PRs via `.github/workflows/benchmarks.yml`:

- Runs full benchmark suite
- Compares with baseline
- Fails if regression > 20%
- Updates baseline on main branch

## Interpreting Results

```
BenchmarkListPlans_Medium-8    20000    50000 ns/op    12000 B/op    120 allocs/op
```

- `20000`: Number of iterations
- `50000 ns/op`: 50 µs per operation
- `12000 B/op`: 12 KB allocated per operation
- `120 allocs/op`: 120 allocations per operation

## Optimization Targets

### High Priority
- Reduce allocations for medium datasets
- Optimize JSON encoding
- Add pagination

### Medium Priority
- Response compression
- Field selection
- Caching headers

## Edge Cases Covered

- Empty datasets
- Large datasets (10K records)
- Mixed data distributions
- Concurrent requests
- Filtered queries

## Security Notes

- Benchmarks use mock data only
- No real database connections
- No external API calls
- Safe for CI/CD pipelines

## Maintenance

### Adding New Benchmarks

```go
func BenchmarkNewFeature(b *testing.B) {
    // Setup
    data := generateData(100)
    
    b.ResetTimer()
    b.ReportAllocs()
    
    for i := 0; i < b.N; i++ {
        // Test code
    }
}
```

### Updating Thresholds

Edit `benchmark_thresholds.go` when:
- Optimizations improve performance
- Requirements change
- Hardware upgrades

## Troubleshooting

### Inconsistent Results

- Increase `-benchtime` (e.g., `-benchtime=10s`)
- Run on dedicated hardware
- Close other applications

### High Variance

- Use `benchstat` for statistical analysis
- Run multiple times
- Check for background processes

## Resources

- [Go Benchmark Documentation](https://pkg.go.dev/testing#hdr-Benchmarks)
- [Benchstat Tool](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
- [Performance Guide](../../BENCHMARK_GUIDE.md)
