package outbox

import (
	"math"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	// MaxTenantLabelLength caps the tenant Prometheus label to bound cardinality
	// and keep scrape payloads small (aligned with middleware cost metrics).
	MaxTenantLabelLength = 64

	// UnknownTenantLabel is used when an outbox event has no tenant_id.
	UnknownTenantLabel = "unknown"

	// IdleTenantLabel is emitted when the backlog is empty so KEDA still sees
	// a zero-valued series and can scale to minReplicaCount.
	IdleTenantLabel = "none"

	// DefaultBacklogPerReplica is the target pending-event load per worker
	// replica used by DesiredWorkerReplicas and the KEDA ScaledObject.
	DefaultBacklogPerReplica = 100

	// DefaultMinWorkerReplicas matches deploy/keda-scaledobject.yaml minReplicaCount.
	DefaultMinWorkerReplicas = 1

	// DefaultMaxWorkerReplicas matches deploy/keda-scaledobject.yaml maxReplicaCount.
	DefaultMaxWorkerReplicas = 20
)

var (
	OutboxPublisherLag            *prometheus.GaugeVec
	OutboxBacklogDepth            *prometheus.GaugeVec
	ChaosOutboxCancellationsTotal prometheus.Counter
)

func init() {
	OutboxPublisherLag = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "outbox_publisher_lag_seconds",
			Help: "Lag in seconds between event occurrence and publisher cursor position per publisher",
		},
		[]string{"publisher"},
	)
	_ = prometheus.Register(OutboxPublisherLag)

	OutboxBacklogDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "outbox_backlog_depth",
			Help: "Number of pending (or retry-due) outbox events by tenant; tenant label is length-capped",
		},
		[]string{"tenant"},
	)
	_ = prometheus.Register(OutboxBacklogDepth)

	ChaosOutboxCancellationsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "chaos_outbox_cancellations_total",
		Help: "Total number of outbox publish cancellations injected by the chaos hook (staging only)",
	})
	_ = prometheus.Register(ChaosOutboxCancellationsTotal)
}

// CapTenantLabel normalizes and truncates a tenant id for Prometheus labels.
func CapTenantLabel(tenant string) string {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		return UnknownTenantLabel
	}
	if len(tenant) > MaxTenantLabelLength {
		return tenant[:MaxTenantLabelLength]
	}
	return tenant
}

// SetOutboxBacklogDepth sets the backlog gauge for a single tenant (label capped).
func SetOutboxBacklogDepth(tenant string, depth float64) {
	if OutboxBacklogDepth == nil {
		return
	}
	OutboxBacklogDepth.WithLabelValues(CapTenantLabel(tenant)).Set(depth)
}

// CountPendingByTenant aggregates pending events by capped tenant label.
func CountPendingByTenant(events []*Event) map[string]int64 {
	depths := make(map[string]int64)
	for _, event := range events {
		if event == nil {
			continue
		}
		label := CapTenantLabel(event.TenantID)
		depths[label]++
	}
	return depths
}

// ObserveOutboxBacklogDepth replaces gauge series with the provided per-tenant
// depths. An empty map writes a single zero series so idle clusters scrape a
// defined metric and KEDA can scale to minReplicaCount.
func ObserveOutboxBacklogDepth(depths map[string]int64) {
	if OutboxBacklogDepth == nil {
		return
	}
	OutboxBacklogDepth.Reset()
	if len(depths) == 0 {
		OutboxBacklogDepth.WithLabelValues(IdleTenantLabel).Set(0)
		return
	}
	for tenant, depth := range depths {
		OutboxBacklogDepth.WithLabelValues(CapTenantLabel(tenant)).Set(float64(depth))
	}
}

// TotalBacklogDepth sums per-tenant backlog counts.
func TotalBacklogDepth(depths map[string]int64) int64 {
	var total int64
	for _, depth := range depths {
		total += depth
	}
	return total
}

// DesiredWorkerReplicas returns the replica count KEDA should converge toward
// for a given total backlog. A zero (or negative) backlog always yields
// minReplicas so idle workers stay at minReplicaCount.
func DesiredWorkerReplicas(totalBacklog float64, minReplicas, maxReplicas int, backlogPerReplica float64) int {
	if minReplicas < 1 {
		minReplicas = DefaultMinWorkerReplicas
	}
	if maxReplicas < minReplicas {
		maxReplicas = minReplicas
	}
	if backlogPerReplica <= 0 {
		backlogPerReplica = DefaultBacklogPerReplica
	}
	if totalBacklog <= 0 {
		return minReplicas
	}
	desired := int(math.Ceil(totalBacklog / backlogPerReplica))
	if desired < minReplicas {
		return minReplicas
	}
	if desired > maxReplicas {
		return maxReplicas
	}
	return desired
}
