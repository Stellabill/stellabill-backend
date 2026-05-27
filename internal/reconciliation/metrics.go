package reconciliation

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	ReconciliationLag *prometheus.GaugeVec
	ReconciliationTotal *prometheus.CounterVec
	ReconciliationReportsTotal *prometheus.CounterVec
)

func init() {
	ReconciliationLag = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "reconciliation_lag_seconds",
			Help: "Lag between contract snapshot and backend update in seconds for stale snapshots",
		},
		[]string{"subscription_id"},
	)

	ReconciliationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "reconciliation_jobs_total",
			Help: "Total number of reconciliation jobs processed",
		},
		[]string{"status"}, // status: success, error
	)

	ReconciliationReportsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "reconciliation_reports_total",
			Help: "Total number of reconciliation reports generated",
		},
		[]string{"matched"}, // matched: true, false
	)

	// Use Register instead of promauto to avoid panics in tests where init might be called multiple times
	// or another package already registered these names.
	_ = prometheus.Register(ReconciliationLag)
	_ = prometheus.Register(ReconciliationTotal)
	_ = prometheus.Register(ReconciliationReportsTotal)
}