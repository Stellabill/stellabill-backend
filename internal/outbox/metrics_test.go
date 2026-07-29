package outbox

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestCapTenantLabel(t *testing.T) {
	require.Equal(t, UnknownTenantLabel, CapTenantLabel(""))
	require.Equal(t, UnknownTenantLabel, CapTenantLabel("   "))
	require.Equal(t, "tenant-a", CapTenantLabel("tenant-a"))

	long := strings.Repeat("x", MaxTenantLabelLength+10)
	capped := CapTenantLabel(long)
	require.Equal(t, MaxTenantLabelLength, len(capped))
	require.Equal(t, long[:MaxTenantLabelLength], capped)
}

func TestObserveOutboxBacklogDepth_ZeroScalesToMinReplicaConcept(t *testing.T) {
	ObserveOutboxBacklogDepth(nil)
	require.Equal(t, 0.0, testutil.ToFloat64(OutboxBacklogDepth.WithLabelValues(IdleTenantLabel)))

	// Conceptual KEDA behavior: backlog stuck at zero → minReplicaCount.
	require.Equal(t, DefaultMinWorkerReplicas, DesiredWorkerReplicas(
		0,
		DefaultMinWorkerReplicas,
		DefaultMaxWorkerReplicas,
		DefaultBacklogPerReplica,
	))
	require.Equal(t, DefaultMinWorkerReplicas, DesiredWorkerReplicas(
		-1,
		DefaultMinWorkerReplicas,
		DefaultMaxWorkerReplicas,
		DefaultBacklogPerReplica,
	))
}

func TestObserveOutboxBacklogDepth_PerTenantAndCap(t *testing.T) {
	long := strings.Repeat("t", MaxTenantLabelLength+5)
	ObserveOutboxBacklogDepth(map[string]int64{
		"tenant-a": 3,
		long:       7,
		"":         2,
	})

	require.Equal(t, 3.0, testutil.ToFloat64(OutboxBacklogDepth.WithLabelValues("tenant-a")))
	require.Equal(t, 7.0, testutil.ToFloat64(OutboxBacklogDepth.WithLabelValues(long[:MaxTenantLabelLength])))
	require.Equal(t, 2.0, testutil.ToFloat64(OutboxBacklogDepth.WithLabelValues(UnknownTenantLabel)))
}

func TestCountPendingByTenant(t *testing.T) {
	events := []*Event{
		{TenantID: "a"},
		{TenantID: "a"},
		{TenantID: "b"},
		{TenantID: ""},
		nil,
	}
	depths := CountPendingByTenant(events)
	require.Equal(t, int64(2), depths["a"])
	require.Equal(t, int64(1), depths["b"])
	require.Equal(t, int64(1), depths[UnknownTenantLabel])
	require.Equal(t, int64(4), TotalBacklogDepth(depths))
}

func TestDesiredWorkerReplicas_ScalesWithBacklog(t *testing.T) {
	require.Equal(t, 1, DesiredWorkerReplicas(50, 1, 20, 100))
	require.Equal(t, 2, DesiredWorkerReplicas(101, 1, 20, 100))
	require.Equal(t, 5, DesiredWorkerReplicas(500, 1, 20, 100))
	require.Equal(t, 20, DesiredWorkerReplicas(10000, 1, 20, 100))
	require.Equal(t, 3, DesiredWorkerReplicas(1, 3, 20, 100), "never below minReplicas")
}

func TestSetOutboxBacklogDepth(t *testing.T) {
	ObserveOutboxBacklogDepth(nil)
	SetOutboxBacklogDepth("acme", 42)
	require.Equal(t, 42.0, testutil.ToFloat64(OutboxBacklogDepth.WithLabelValues("acme")))
}
