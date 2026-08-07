# Tracing Sampler Benchmarks

## Running Benchmarks
```bash
go test ./pkg/tracing/... -bench=. -benchmem -benchtime=10s
```

## Performance Targets
| Operation | Target | P99 |
|-----------|--------|-----|
| ShouldSample() | < 1μs | < 5μs |
| tierCache.Get() | < 100ns | < 500ns |
| GetSamplingRate() | < 50ns | < 200ns |

## Baseline Results (expected)
```
BenchmarkShouldSample/Free-8         50000000    25 ns/op    0 allocs/op
BenchmarkShouldSample/Pro-8          50000000    28 ns/op    0 allocs/op
BenchmarkShouldSample/Enterprise-8   50000000    24 ns/op    0 allocs/op
BenchmarkTierCache_Get-8            100000000    12 ns/op    0 allocs/op
```
