package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	WebhookInboxLag = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "webhook_inbox_lag_seconds",
		Help:    "Time elapsed between receiving a webhook and successfully processing it",
		Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300},
	})
)