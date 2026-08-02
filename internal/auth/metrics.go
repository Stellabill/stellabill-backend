package auth

import "github.com/prometheus/client_golang/prometheus"

// jwksRefreshErrorsTotal counts failed JWKS refresh attempts against the
// configured issuer. A rising rate indicates the issuer is unreachable or
// misbehaving; combined with the negative-cache cap and refresh rate limit,
// this is what an on-call engineer should alert on.
var jwksRefreshErrorsTotal prometheus.Counter

func init() {
	jwksRefreshErrorsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "jwks_refresh_errors_total",
		Help: "Total number of failed JWKS refresh attempts against the configured issuer",
	})
	_ = prometheus.Register(jwksRefreshErrorsTotal)
}