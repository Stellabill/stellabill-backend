package outbox

import (
	"math"
	"math/rand"
	"time"
)

// CalculateNextRetry returns the backoff duration for a given retry count.
// Formula: base * factor^retryCount + uniform_jitter([0, base))
// where base = cfg.RetryBaseDelay and factor = cfg.RetryBackoffFactor.
func CalculateNextRetry(retryCount int, cfg DispatcherConfig) time.Duration {
	base := cfg.RetryBaseDelay
	factor := cfg.RetryBackoffFactor

	deterministic := float64(base) * math.Pow(factor, float64(retryCount))
	jitter := rand.Float64() * float64(base) // uniform in [0, base)

	return time.Duration(deterministic + jitter)
}
