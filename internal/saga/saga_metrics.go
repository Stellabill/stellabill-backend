package saga

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	SagaStepRetriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "saga_step_retries_total",
			Help: "Total number of saga step retries by flow and step",
		},
		[]string{"flow", "step"},
	)

	SagaStepRetryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "saga_step_retry_duration_seconds",
			Help:    "Duration spent retrying saga steps by flow and step",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"flow", "step"},
	)
)
