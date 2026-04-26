package httpclient

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTPClientRetriesTotal tracks the number of times requests are retried.
	HTTPClientRetriesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_client_retries_total",
			Help: "Total number of retries made by the resilient HTTP client",
		},
		[]string{"host", "method"},
	)

	// HTTPClientFailuresTotal tracks the number of times requests ultimately fail (including max retries reached).
	HTTPClientFailuresTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_client_failures_total",
			Help: "Total number of failures from the resilient HTTP client",
		},
		[]string{"host", "method", "reason"},
	)

	// HTTPClientCircuitState tracks the current state of circuit breakers.
	// 0 = Closed, 1 = Open, 2 = HalfOpen.
	HTTPClientCircuitState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_client_circuit_state",
			Help: "Current state of the circuit breaker (0=Closed, 1=Open, 2=HalfOpen)",
		},
		[]string{"host"},
	)
)
