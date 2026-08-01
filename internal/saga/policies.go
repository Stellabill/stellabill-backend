package saga

import "time"

// DefaultRetryPolicy returns the default per-step retry policy.
// The RetryPolicy type itself is declared in saga.go.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Second,
		Jitter:      0.2,
	}
}
