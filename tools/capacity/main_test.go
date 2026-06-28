package main

import (
	"testing"
	"time"
)

const baselineMetrics = `
# HELP process_cpu_seconds_total Total user and system CPU time spent in seconds.
process_cpu_seconds_total 120
process_resident_memory_bytes 268435456
db_queries_total{operation="SELECT",table="subscriptions",error="false"} 200
db_queries_total{operation="INSERT",table="subscriptions",error="false"} 20
pg_stat_database_blks_read 4000
pg_stat_database_blks_written 500
`

const peakMetrics = `
process_cpu_seconds_total 180
process_resident_memory_bytes 402653184
db_queries_total{operation="SELECT",table="subscriptions",error="false"} 520
db_queries_total{operation="INSERT",table="subscriptions",error="false"} 80
pg_stat_database_blks_read 7600
pg_stat_database_blks_written 1000
`

func TestBuildPlan_ZeroTrafficProfile(t *testing.T) {
	base, err := newSnapshotFromString(`
process_cpu_seconds_total 25
process_resident_memory_bytes 201326592
pg_stat_database_blks_read 100
pg_stat_database_blks_written 50
`)
	if err != nil {
		t.Fatalf("parse baseline: %v", err)
	}

	peak, err := newSnapshotFromString(`
process_cpu_seconds_total 25
process_resident_memory_bytes 201326592
pg_stat_database_blks_read 100
pg_stat_database_blks_written 50
`)
	if err != nil {
		t.Fatalf("parse peak: %v", err)
	}

	plan, err := BuildPlan(Inputs{
		Base:            base,
		Peak:            peak,
		Window:          10 * time.Minute,
		ObservedTenants: 1,
		TenantCounts:    []int{0, 10},
		PodCPUMilli:     500,
		PodMemoryMiB:    1024,
		Headroom:        1.25,
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	if plan.PerTenantCPUm != 0 {
		t.Fatalf("expected zero CPU delta, got %f", plan.PerTenantCPUm)
	}
	if plan.PerTenantPostgresIOPS != 0 {
		t.Fatalf("expected zero IOPS delta, got %f", plan.PerTenantPostgresIOPS)
	}
	if len(plan.Recommendations) != 2 {
		t.Fatalf("expected two recommendations, got %d", len(plan.Recommendations))
	}
	if plan.Recommendations[0].AppReplicas != 1 {
		t.Fatalf("expected one replica at zero tenants, got %d", plan.Recommendations[0].AppReplicas)
	}
}

func TestBuildPlan_BurstTrafficProfile(t *testing.T) {
	base, err := newSnapshotFromString(baselineMetrics)
	if err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	peak, err := newSnapshotFromString(peakMetrics)
	if err != nil {
		t.Fatalf("parse peak: %v", err)
	}

	plan, err := BuildPlan(Inputs{
		Base:            base,
		Peak:            peak,
		Window:          15 * time.Minute,
		ObservedTenants: 50,
		TenantCounts:    []int{50, 250},
		PodCPUMilli:     500,
		PodMemoryMiB:    1024,
		Headroom:        1.25,
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	if plan.PerTenantCPUm <= 0 {
		t.Fatalf("expected CPU cost to be positive")
	}
	if plan.PerTenantMemoryMiB <= 0 {
		t.Fatalf("expected memory cost to be positive")
	}
	if plan.PerTenantPostgresIOPS <= 0 {
		t.Fatalf("expected IOPS cost to be positive")
	}

	if plan.Recommendations[1].AppReplicas < plan.Recommendations[0].AppReplicas {
		t.Fatalf("expected larger tenant counts to require at least as many replicas")
	}
}

func TestParseTenantCounts(t *testing.T) {
	counts, err := parseTenantCounts("10, 25, 100", 0)
	if err != nil {
		t.Fatalf("parse counts: %v", err)
	}
	if len(counts) != 3 || counts[0] != 10 || counts[2] != 100 {
		t.Fatalf("unexpected counts: %#v", counts)
	}
}
