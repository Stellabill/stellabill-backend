package saga

import (
	"context"
	"math/rand"
	"time"
)

var StepRetriesTotal = func(flow, step string) {}

// ExecuteStep runs a single saga step with its retry policy. The Step type is
// declared in saga.go; this helper executes step.Execute with a fresh
// SagaContext and applies exponential backoff with jitter on failure.
func ExecuteStep(ctx context.Context, flowName string, step Step) error {
	policy := step.RetryPolicy
	if policy == nil || policy.MaxAttempts <= 0 {
		p := DefaultRetryPolicy()
		policy = &p
	}

	sagaCtx := NewSagaContext(nil)
	var err error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		err = step.Execute(ctx, sagaCtx)
		if err == nil {
			return nil
		}

		if attempt < policy.MaxAttempts {
			StepRetriesTotal(flowName, step.Key)
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
