package main

import (
	"fmt"
	"math"
	"time"
)

const (
	mib = 1024 * 1024
)

type Inputs struct {
	Base            Snapshot
	Peak            Snapshot
	Window          time.Duration
	ObservedTenants int
	TenantCounts    []int
	PodCPUMilli     int
	PodMemoryMiB    int
	Headroom        float64
}

type Plan struct {
	ObservedTenants       int              `json:"observed_tenants"`
	Window                string           `json:"window"`
	PerTenantCPUm         float64          `json:"per_tenant_cpu_mcores"`
	PerTenantMemoryMiB    float64          `json:"per_tenant_memory_mib"`
	PerTenantPostgresIOPS float64          `json:"per_tenant_postgres_iops"`
	PerTenantDBQueriesQPS float64          `json:"per_tenant_db_queries_qps"`
	Recommendations       []Recommendation `json:"recommendations"`
}

type Recommendation struct {
	Tenants              int `json:"tenants"`
	RequiredCPUMilli     int `json:"required_cpu_milli"`
	RequiredMemoryMiB    int `json:"required_memory_mib"`
	RequiredPostgresIOPS int `json:"required_postgres_iops"`
	AppReplicas          int `json:"app_replicas"`
}

func BuildPlan(in Inputs) (Plan, error) {
	if in.Window <= 0 {
		return Plan{}, fmt.Errorf("window must be greater than zero")
	}
	if in.Headroom < 1 {
		return Plan{}, fmt.Errorf("headroom must be at least 1.0")
	}
	if in.PodCPUMilli <= 0 {
		return Plan{}, fmt.Errorf("pod CPU limit must be positive")
	}
	if in.PodMemoryMiB <= 0 {
		return Plan{}, fmt.Errorf("pod memory limit must be positive")
	}
	if len(in.TenantCounts) == 0 {
		return Plan{}, fmt.Errorf("at least one tenant count is required")
	}

	baseCPUSeconds := in.Base.Sum("process_cpu_seconds_total", nil)
	peakCPUSeconds := in.Peak.Sum("process_cpu_seconds_total", nil)
	baseMemoryBytes := in.Base.Sum("process_resident_memory_bytes", nil)
	peakMemoryBytes := in.Peak.Sum("process_resident_memory_bytes", nil)

	basePostgresOps := postgresIOPS(in.Base)
	peakPostgresOps := postgresIOPS(in.Peak)

	baseDBQueries := in.Base.Sum("db_queries_total", nil)
	peakDBQueries := in.Peak.Sum("db_queries_total", nil)

	observedTenants := in.ObservedTenants
	if observedTenants < 0 {
		return Plan{}, fmt.Errorf("observed tenants cannot be negative")
	}

	tenantFactor := 0.0
	if observedTenants > 0 {
		tenantFactor = 1 / float64(observedTenants)
	}

	cpuDelta := math.Max(0, peakCPUSeconds-baseCPUSeconds)
	memDeltaBytes := math.Max(0, peakMemoryBytes-baseMemoryBytes)
	iopsDelta := math.Max(0, peakPostgresOps-basePostgresOps)
	dbQueryDelta := math.Max(0, peakDBQueries-baseDBQueries)

	perTenantCPUm := (cpuDelta / in.Window.Seconds() * 1000) * tenantFactor
	perTenantMemoryMiB := (memDeltaBytes / mib) * tenantFactor
	perTenantIOPS := (iopsDelta / in.Window.Seconds()) * tenantFactor
	perTenantDBQueries := (dbQueryDelta / in.Window.Seconds()) * tenantFactor

	recommendations := make([]Recommendation, 0, len(in.TenantCounts))
	for _, tenants := range in.TenantCounts {
		totalCPUm := int(math.Ceil(perTenantCPUm * float64(tenants) * in.Headroom))
		totalMemoryMiB := int(math.Ceil(((baseMemoryBytes / mib) + perTenantMemoryMiB*float64(tenants)) * in.Headroom))
		totalIOPS := int(math.Ceil(perTenantIOPS * float64(tenants) * in.Headroom))

		replicasByCPU := int(math.Ceil(float64(totalCPUm) / float64(in.PodCPUMilli)))
		replicasByMemory := int(math.Ceil(float64(totalMemoryMiB) / float64(in.PodMemoryMiB)))
		appReplicas := maxInt(1, maxInt(replicasByCPU, replicasByMemory))
		if tenants == 0 {
			appReplicas = maxInt(1, replicasByMemory)
		}

		recommendations = append(recommendations, Recommendation{
			Tenants:              tenants,
			RequiredCPUMilli:     maxInt(0, totalCPUm),
			RequiredMemoryMiB:    maxInt(0, totalMemoryMiB),
			RequiredPostgresIOPS: maxInt(0, totalIOPS),
			AppReplicas:          appReplicas,
		})
	}

	return Plan{
		ObservedTenants:       observedTenants,
		Window:                in.Window.String(),
		PerTenantCPUm:         perTenantCPUm,
		PerTenantMemoryMiB:    perTenantMemoryMiB,
		PerTenantPostgresIOPS: perTenantIOPS,
		PerTenantDBQueriesQPS: perTenantDBQueries,
		Recommendations:       recommendations,
	}, nil
}

func postgresIOPS(s Snapshot) float64 {
	return s.Sum("pg_stat_database_blks_read", nil) + s.Sum("pg_stat_database_blks_written", nil)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
