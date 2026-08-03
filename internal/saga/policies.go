package saga

import "time"

type StepRetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	Jitter      float64 // percentage or factor, e.g., 0.2 for 20%
}

func DefaultStepRetryPolicy() StepRetryPolicy {
	return StepRetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Second,
		Jitter:      0.2,
	}
}
