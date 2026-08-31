package saga

import (
	"math/rand"
	"time"
)

// RetryPolicy controls how many times a failing saga step is retried and the
// delay between attempts. The wait before a retry grows linearly with the retry
// count (BaseDelay * retries) and Jitter (0.0–1.0) adds randomness around that
// value so concurrent retries do not stampede.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Jitter      float64
}

// DefaultRetry returns the default retry policy: up to 3 attempts with a 100ms
// base delay, 20% jitter, capped at 2 seconds.
func DefaultRetry() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    2 * time.Second,
		Jitter:      0.2,
	}
}

// DefaultRetryPolicy is an alias for DefaultRetry.
func DefaultRetryPolicy() RetryPolicy {
	return DefaultRetry()
}

// ShouldRetry reports whether one more attempt should be made given the number
// of retries already performed. A nil policy never retries. A step with
// MaxAttempts=1 never retries because other retry would exceed MaxAttempts.
func (p *RetryPolicy) ShouldRetry(retries int) bool {
	if p == nil {
		return false
	}
	return retries < p.MaxAttempts && retries >= 0
}

// NextDelay returns how long to wait before the next attempt given the
// 1-based retry count. The delay is BaseDelay * retries randomized by Jitter
// and capped at MaxDelay. A nil policy returns zero.
func (p *RetryPolicy) NextDelay(retries int) time.Duration {
	if p == nil {
		return 0
	}
	base := float64(p.BaseDelay) * float64(retries)
	if base <= 0 {
		return 0
	}
	span := base * p.Jitter
	delay := time.Duration(base - span + 2*span*rand.Float64())
	if p.MaxDelay > 0 && delay > p.MaxDelay {
		return p.MaxDelay
	}
	return delay
}
