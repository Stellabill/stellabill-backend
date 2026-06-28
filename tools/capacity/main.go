package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	var (
		basePath   = flag.String("base", "", "path to the baseline Prometheus snapshot")
		peakPath   = flag.String("peak", "", "path to the production Prometheus snapshot")
		window     = flag.Duration("window", 15*time.Minute, "measurement window between snapshots")
		tenants    = flag.Int("tenants", 0, "observed tenant count for the production snapshot")
		counts     = flag.String("tenant-counts", "", "comma-separated tenant counts to size (defaults to -tenants)")
		podCPU     = flag.Int("pod-cpu-milli", 500, "CPU limit per app pod in millicores")
		podMemory  = flag.Int("pod-memory-mib", 1024, "memory limit per app pod in MiB")
		headroom   = flag.Float64("headroom", 1.25, "safety multiplier applied to the measured totals")
		outputJSON = flag.Bool("json", false, "emit JSON instead of a table")
	)
	flag.Parse()

	if *basePath == "" || *peakPath == "" {
		fmt.Fprintln(os.Stderr, "usage: capacity -base baseline.prom -peak peak.prom [-window 15m] [-tenants 100] [-tenant-counts 100,250,500]")
		os.Exit(2)
	}

	baseSnapshot, err := readSnapshotFile(*basePath)
	must(err)

	peakSnapshot, err := readSnapshotFile(*peakPath)
	must(err)

	observedTenants := *tenants
	if observedTenants < 0 {
		fatalf("-tenants cannot be negative")
	}

	tenantCounts, err := parseTenantCounts(*counts, observedTenants)
	must(err)

	plan, err := BuildPlan(Inputs{
		Base:            baseSnapshot,
		Peak:            peakSnapshot,
		Window:          *window,
		ObservedTenants: observedTenants,
		TenantCounts:    tenantCounts,
		PodCPUMilli:     *podCPU,
		PodMemoryMiB:    *podMemory,
		Headroom:        *headroom,
	})
	must(err)

	if *outputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		must(enc.Encode(plan))
		return
	}

	fmt.Println("Capacity planning summary")
	fmt.Printf("Observed tenants: %d\n", plan.ObservedTenants)
	fmt.Printf("Window: %s\n", plan.Window)
	fmt.Printf("Per-tenant CPU: %.2f mcores\n", plan.PerTenantCPUm)
	fmt.Printf("Per-tenant memory: %.2f MiB\n", plan.PerTenantMemoryMiB)
	fmt.Printf("Per-tenant Postgres IOPS: %.2f\n", plan.PerTenantPostgresIOPS)
	fmt.Printf("Per-tenant DB queries: %.2f qps\n", plan.PerTenantDBQueriesQPS)
	fmt.Println()
	fmt.Printf("%-10s %-12s %-14s %-16s %-10s\n", "tenants", "cpu_mcores", "memory_mib", "postgres_iops", "replicas")
	for _, rec := range plan.Recommendations {
		fmt.Printf("%-10d %-12d %-14d %-16d %-10d\n",
			rec.Tenants, rec.RequiredCPUMilli, rec.RequiredMemoryMiB, rec.RequiredPostgresIOPS, rec.AppReplicas)
	}
}

func parseTenantCounts(raw string, fallback int) ([]int, error) {
	if strings.TrimSpace(raw) == "" {
		return []int{fallback}, nil
	}

	parts := strings.Split(raw, ",")
	counts := make([]int, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("parse tenant count %q: %w", value, err)
		}
		if n < 0 {
			return nil, fmt.Errorf("tenant count %d cannot be negative", n)
		}
		counts = append(counts, n)
	}
	if len(counts) == 0 {
		return []int{fallback}, nil
	}
	return counts, nil
}

func must(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
