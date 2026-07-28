package outbox

import "github.com/prometheus/client_golang/prometheus"

var (
	OutboxPublisherLag            *prometheus.GaugeVec
	OutboxPublisherInflight       prometheus.Gauge
	OutboxPublisherLimit          prometheus.Gauge
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

	OutboxPublisherInflight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "outbox_publisher_inflight",
			Help: "Number of in-flight publish requests to the downstream HTTP service",
		},
	)
	_ = prometheus.Register(OutboxPublisherInflight)

	OutboxPublisherLimit = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "outbox_publisher_limit",
			Help: "Current adaptive concurrency limit for the HTTP publisher",
		},
	)
	_ = prometheus.Register(OutboxPublisherLimit)

	ChaosOutboxCancellationsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "chaos_outbox_cancellations_total",
		Help: "Total number of outbox publish cancellations injected by the chaos hook (staging only)",
	})
	_ = prometheus.Register(ChaosOutboxCancellationsTotal)
}
