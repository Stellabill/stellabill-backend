package handlers

// BenchmarkThresholds defines performance regression thresholds
type BenchmarkThresholds struct {
	MaxLatencyNs int64
	MaxAllocsOp  int64
	MaxBytesOp   int64
}

// Thresholds for different dataset sizes
var (
	ThresholdPlansSmall = BenchmarkThresholds{
		MaxLatencyNs: 30000, // 30 µs
		MaxAllocsOp:  25,    // 25 allocations
		MaxBytesOp:   15000, // 15 KB
	}

	ThresholdPlansMedium = BenchmarkThresholds{
		MaxLatencyNs: 150000, // 150 µs
		MaxAllocsOp:  200,    // 200 allocations
		MaxBytesOp:   120000, // 120 KB
	}

	ThresholdPlansLarge = BenchmarkThresholds{
		MaxLatencyNs: 1500000, // 1.5 ms
		MaxAllocsOp:  2000,    // 2000 allocations
		MaxBytesOp:   1200000, // 1.2 MB
	}

	ThresholdSubscriptionsSmall = BenchmarkThresholds{
		MaxLatencyNs: 35000, // 35 µs
		MaxAllocsOp:  30,    // 30 allocations
		MaxBytesOp:   18000, // 18 KB
	}

	ThresholdSubscriptionsMedium = BenchmarkThresholds{
		MaxLatencyNs: 165000, // 165 µs
		MaxAllocsOp:  220,    // 220 allocations
		MaxBytesOp:   140000, // 140 KB
	}

	ThresholdSubscriptionsLarge = BenchmarkThresholds{
		MaxLatencyNs: 1650000, // 1.65 ms
		MaxAllocsOp:  2200,    // 2200 allocations
		MaxBytesOp:   1400000, // 1.4 MB
	}
)
