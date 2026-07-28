package httpx

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// connReuseRatio tracks, per host, the fraction of requests that reused a
// pooled connection instead of dialing a new one (0-1). A ratio that stays
// near zero for a host under steady load usually means its connections are
// being recycled too aggressively (or the upstream is closing them).
var connReuseRatio = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "http_client_conn_reuse_ratio",
		Help: "Ratio of pooled HTTP client requests that reused an existing connection, per host.",
	},
	[]string{"host"},
)
