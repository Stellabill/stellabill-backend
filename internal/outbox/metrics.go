package outbox

import "github.com/prometheus/client_golang/prometheus"

var (
	OutboxPublisherLag *prometheus.GaugeVec
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
}
