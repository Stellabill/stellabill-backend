package saga

import (
	"math"
	"math/rand"
	"time"
)

var (
	DefaultRetryPolicy = RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    5 * time.Second,
		Jitter:      0.2,
	}

	NoRetryPolicy = RetryPolicy{
		MaxAttempts: 1,
	}
)

func DefaultRetry() *RetryPolicy {
	p := DefaultRetryPolicy
	return &p
}

func (p *RetryPolicy) ShouldRetry(attempt int) bool {
	if p == nil {
		return false
	}
	return attempt < p.MaxAttempts
}

func (p *RetryPolicy) NextDelay(attempt int) time.Duration {
	if p == nil {
		return 0
	}
	if p.BaseDelay <= 0 || attempt <= 0 {
		return 0
	}

	base := float64(p.BaseDelay)
	delay := base * math.Pow(2, float64(attempt-1))
	if delay > float64(p.MaxDelay) {
		delay = float64(p.MaxDelay)
	}

	if p.Jitter > 0 {
		jitterRange := delay * p.Jitter
		jitter := (rand.Float64()*2 - 1) * jitterRange
		delay += jitter
		if delay < 0 {
			delay = 0
		}
	}

	d := time.Duration(delay)
	if d < time.Millisecond {
		d = time.Millisecond
	}
	return d
}
