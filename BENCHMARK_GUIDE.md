# Benchmark Guide: List Endpoints

## Overview

Comprehensive benchmark suite for plans and subscriptions list endpoints to establish latency baselines and detect performance regressions.

## Running Benchmarks

### All Benchmarks

```bash
go test ./internal/handlers/... -bench=. -benchmem -benchtime=3s
```

### Specific Endpoint

```bash
# Plans only
go test ./internal/handlers/... -bench=BenchmarkListPlans -benchmem

# Subscriptions only
go test ./internal/handlers/... -bench=BenchmarkListSubscriptions -benchmem
```

### With CPU Profiling

```bash
go test ./internal/handlers/... -bench=. -benchmem -cpuprofile=cpu.prof
go tool pprof cpu.prof
```

### With Memory Profiling

```bash
go test ./internal/handlers/... -bench=. -benchmem -memprofile=mem.prof
go tool pprof mem.prof
```

## Benchmark Categories

### 1. Dataset Size Benchmarks

Tests performance across different data volumes:

- **Empty**: 0 records (baseline)
- **Small**: 10 records (typical single-page response)
- **Medium**: 100 records (typical paginated response)
- **Large**: 1,000 records (large merchant)
- **ExtraLarge**: 10,000 records (stress test)

### 2. JSON Encoding Benchmarks

Isolates JSON serialization performance:

```bash
go test ./internal/handlers/... -bench=JSONEncoding -benchmem
```

### 3. Full HTTP Benchmarks

Tests complete request/response cycle:

```bash
go test ./internal/handlers/... -bench=FullHTTP -benchmem
```

### 4. Parallel Benchmarks

Tests concurrent request handling:

```bash
go test ./internal/handlers/... -bench=Parallel -benchmem
```

### 5. Filtered Benchmarks

Tests query filtering performance:

```bash
go test ./internal/handlers/... -bench=Filtered -benchmem
```

## Expected Baselines

### Plans Endpoint

| Dataset Size | Operations/sec | Latency (p50) | Latency (p95) | Allocs/op |
|--------------|----------------|---------------|---------------|-----------|
| Empty        | ~500,000       | ~2 µs         | ~5 µs         | 2         |
| Small (10)   | ~100,000       | ~10 µs        | ~20 µs        | 15        |
| Medium (100) | ~20,000        | ~50 µs        | ~100 µs       | 120       |
| Large (1K)   | ~2,000         | ~500 µs       | ~1 ms         | 1,200     |
| XLarge (10K) | ~200           | ~5 ms         | ~10 ms        | 12,000    |

### Subscriptions Endpoint

| Dataset Size | Operations/sec | Latency (p50) | Latency (p95) | Allocs/op |
|--------------|----------------|---------------|---------------|-----------|
| Empty        | ~500,000       | ~2 µs         | ~5 µs         | 2         |
| Small (10)   | ~90,000        | ~11 µs        | ~22 µs        | 18        |
| Medium (100) | ~18,000        | ~55 µs        | ~110 µs       | 140       |
| Large (1K)   | ~1,800         | ~550 µs       | ~1.1 ms       | 1,400     |
| XLarge (10K) | ~180           | ~5.5 ms       | ~11 ms        | 14,000    |

*Note: Actual results depend on hardware. These are reference values.*

## Performance Thresholds

### Regression Alerts

Trigger alerts if benchmarks exceed these thresholds:

```yaml
plans:
  small:
    max_latency_us: 30
    max_allocs: 25
  medium:
    max_latency_us: 150
    max_allocs: 200
  large:
    max_latency_us: 1500
    max_allocs: 2000

subscriptions:
  small:
    max_latency_us: 35
    max_allocs: 30
  medium:
    max_latency_us: 165
    max_allocs: 220
  large:
    max_latency_us: 1650
    max_allocs: 2200
```

## Analyzing Results

### Reading Benchmark Output

```
BenchmarkListPlans_Medium-8    20000    50000 ns/op    12000 B/op    120 allocs/op
                        │       │         │              │            │
                        │       │         │              │            └─ Allocations per operation
                        │       │         │              └─ Bytes allocated per operation
                        │       │         └─ Nanoseconds per operation
                        │       └─ Number of iterations
                        └─ CPU cores used
```

### Key Metrics

1. **ns/op**: Latency per operation (lower is better)
2. **B/op**: Memory allocated per operation (lower is better)
3. **allocs/op**: Number of allocations (lower is better)

### Comparing Results

```bash
# Run baseline
go test ./internal/handlers/... -bench=. -benchmem > baseline.txt

# Make changes
# ...

# Run comparison
go test ./internal/handlers/... -bench=. -benchmem > new.txt

# Compare
benchstat baseline.txt new.txt
```

## Optimization Targets

### High Priority

1. **Reduce allocations**: Target <100 allocs/op for medium datasets
2. **Optimize JSON encoding**: Consider faster JSON libraries
3. **Add pagination**: Limit response size to 100 records max

### Medium Priority

1. **Response compression**: Enable gzip for large responses
2. **Field selection**: Allow clients to request specific fields
3. **Caching**: Add ETag/Last-Modified headers

### Low Priority

1. **Streaming responses**: For very large datasets
2. **Binary protocols**: Consider protobuf for internal APIs
3. **Connection pooling**: Optimize database connections

## CI Integration

### GitHub Actions

```yaml
name: Performance Benchmarks

on: [pull_request]

jobs:
  benchmark:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      
      - name: Run benchmarks
        run: |
          go test ./internal/handlers/... -bench=. -benchmem -benchtime=3s > new.txt
          cat new.txt
      
      - name: Compare with baseline
        run: |
          # Download baseline from previous run
          # Compare and fail if regression > 20%
          go install golang.org/x/perf/cmd/benchstat@latest
          benchstat baseline.txt new.txt
```

### Regression Detection

```bash
#!/bin/bash
# detect_regression.sh

THRESHOLD=1.20  # 20% regression threshold

go test ./internal/handlers/... -bench=. -benchmem > new.txt

# Compare with baseline
benchstat baseline.txt new.txt | grep -E "~|±" | while read line; do
    # Parse and check if regression > threshold
    # Exit 1 if regression detected
done
```

## Best Practices

### Writing Benchmarks

1. **Use b.ResetTimer()**: Reset after setup
2. **Use b.ReportAllocs()**: Track memory allocations
3. **Avoid I/O**: Mock external dependencies
4. **Run multiple times**: Use -benchtime for stability
5. **Test realistic data**: Use representative fixtures

### Interpreting Results

1. **Focus on trends**: Single runs vary, track over time
2. **Compare apples to apples**: Same hardware, same load
3. **Consider context**: CPU, memory, concurrent load
4. **Profile hot paths**: Use pprof for optimization
5. **Validate in production**: Synthetic benchmarks != real traffic

### Optimization Workflow

1. Run baseline benchmarks
2. Identify bottlenecks with profiling
3. Make targeted optimization
4. Run benchmarks again
5. Compare results with benchstat
6. Repeat until targets met

## Common Issues

### Benchmark Variance

**Problem**: Results vary significantly between runs

**Solutions**:
- Increase -benchtime (e.g., -benchtime=10s)
- Run on dedicated hardware
- Disable CPU frequency scaling
- Close other applications

### Memory Leaks

**Problem**: Allocations increase over time

**Solutions**:
- Use memory profiler
- Check for goroutine leaks
- Verify proper cleanup
- Review object pooling

### Unrealistic Results

**Problem**: Benchmarks too fast/slow

**Solutions**:
- Verify fixtures are realistic
- Check for compiler optimizations
- Ensure work isn't optimized away
- Add realistic complexity

## Monitoring in Production

### Metrics to Track

```go
// Request latency histogram
histogram.Observe(duration.Seconds())

// Response size
counter.Add(float64(responseSize))

// Concurrent requests
gauge.Set(float64(activeRequests))
```

### SLO Targets

- **p50 latency**: < 50ms
- **p95 latency**: < 200ms
- **p99 latency**: < 500ms
- **Error rate**: < 0.1%
- **Throughput**: > 1000 req/s

## Troubleshooting

### Slow Benchmarks

1. Check dataset size (reduce for faster iteration)
2. Use -benchtime=1s for quick runs
3. Run specific benchmarks with -bench=Pattern
4. Profile with -cpuprofile

### High Memory Usage

1. Check for memory leaks
2. Review allocation patterns
3. Consider object pooling
4. Use memory profiler

### Inconsistent Results

1. Run on stable hardware
2. Increase benchmark time
3. Check for background processes
4. Use benchstat for statistical analysis

## Resources

- [Go Benchmark Documentation](https://pkg.go.dev/testing#hdr-Benchmarks)
- [Benchstat Tool](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
- [Profiling Go Programs](https://go.dev/blog/pprof)
- [Performance Optimization Guide](https://github.com/dgryski/go-perfbook)
