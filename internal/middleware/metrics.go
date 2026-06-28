package middleware

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	IdempotencyKeysPurgedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "idempotency_keys_purged_total",
		Help: "Total number of expired idempotency keys purged",
	})

	IdempotencyKeysExpiredPending = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "idempotency_keys_expired_pending",
		Help: "Number of expired idempotency keys pending deletion",
	})
)
