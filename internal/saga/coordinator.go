package saga

import (
	"context"
	"math/rand"
	"time"
)

type Step struct {
	Name        string
	Action      func(ctx context.Context) error
	Compensate  func(ctx context.Context) error
	RetryPolicy RetryPolicy
}

var StepRetriesTotal = func(flow, step string) {}

func ExecuteStep(ctx context.Context, flowName string, step Step) error {
	policy := step.RetryPolicy
	if policy.MaxAttempts <= 0 {
		policy = DefaultRetryPolicy()
	}

	var err error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		err = step.Action(ctx)
		if err == nil {
			return nil
		}

		if attempt < policy.MaxAttempts {
			StepRetriesTotal(flowName, step.Name)
			delay := float64(policy.BaseDelay) * float64(attempt)
			jitterVal := delay * policy.Jitter * (rand.Float64()*2 - 1)
			sleepDuration := time.Duration(delay + jitterVal)
			
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(sleepDuration):
			}
		}
	}
	return err
}
