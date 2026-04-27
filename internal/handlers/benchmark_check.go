package handlers

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type BenchmarkResult struct {
	Name      string
	NsPerOp  int64
	AllocsOp int64
	BPerOp   int64
}

type ThresholdConfig struct {
	MaxLatencyNs int64
	MaxAllocsOp int64
	MaxBytesOp  int64
}

var (
	DefaultThreshold = ThresholdConfig{
		MaxLatencyNs: 100000, // 100 µs
		MaxAllocsOp:  100,
		MaxBytesOp:   50000,
	}

	ThresholdByBenchmark = map[string]ThresholdConfig{
		"ListPlans": {
			MaxLatencyNs: 30000,
			MaxAllocsOp:  25,
			MaxBytesOp:   15000,
		},
		"ListSubscriptions": {
			MaxLatencyNs: 35000,
			MaxAllocsOp:  30,
			MaxBytesOp:   18000,
		},
		"ListStatements": {
			MaxLatencyNs: 50000,
			MaxAllocsOp:  40,
			MaxBytesOp:   25000,
		},
	}
)

func GetThresholdForBenchmark(name string) ThresholdConfig {
	if th, ok := ThresholdByBenchmark[name]; ok {
		return th
	}
	return DefaultThreshold
}

func (r BenchmarkResult) ExceedsThreshold(th ThresholdConfig) (bool, string) {
	if r.NsPerOp > th.MaxLatencyNs {
		return true, fmt.Sprintf("latency %dns exceeds threshold %dns", r.NsPerOp, th.MaxLatencyNs)
	}
	if r.AllocsOp > th.MaxAllocsOp {
		return true, fmt.Sprintf("allocations %d exceeds threshold %d", r.AllocsOp, th.MaxAllocsOp)
	}
	if r.BPerOp > th.MaxBytesOp {
		return true, fmt.Sprintf("bytes %d exceeds threshold %d", r.BPerOp, th.MaxBytesOp)
	}
	return false, ""
}

func ParseBenchmarkOutput(output string) ([]BenchmarkResult, error) {
	var results []BenchmarkResult
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		if !strings.Contains(line, "ns/op") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		name := strings.TrimSuffix(fields[0], "_")

		nsOp := strings.Replace(fields[1], "ns/op", "", 1)
		ns, _ := strconv.ParseInt(nsOp, 10, 64)

		allocs := int64(0)
		if len(fields) >= 3 {
			allocsStr := strings.Replace(fields[2], "B/op", "", 1)
			allocs, _ = strconv.ParseInt(allocsStr, 10, 64)
		}

		bytes := int64(0)
		if len(fields) >= 4 {
			bytesStr := strings.Replace(fields[3], "MB/op", "", 1)
			bytes, _ = strconv.ParseInt(bytesStr, 10, 64)
		}

		results = append(results, BenchmarkResult{
			Name:      name,
			NsPerOp:   ns,
			AllocsOp: allocs,
			BPerOp:   bytes,
		})
	}

	return results, nil
}

func VerifyBenchmarks(output string) error {
	results, err := ParseBenchmarkOutput(output)
	if err != nil {
		return err
	}

	var failures []string
	for _, result := range results {
		th := GetThresholdForBenchmark(result.Name)
		if exceeds, msg := result.ExceedsThreshold(th); exceeds {
			failures = append(failures, fmt.Sprintf("%s: %s", result.Name, msg))
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("benchmark threshold violations:\n%s", strings.Join(failures, "\n"))
	}

	return nil
}

func InitBenchmarkTracking() {
	_ = os.Setenv("BENCHMARK_OUTPUT", time.Now().Format("20060102_150405"))
}