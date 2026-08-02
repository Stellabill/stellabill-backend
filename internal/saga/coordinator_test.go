package saga

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryPolicySuccess(t *testing.T) {
	attempts := 0
	step := Step{
		Name: "test-step",
		Action: func(ctx context.Context) error {
			attempts++
			if attempts < 3 {
				return errors.New("temporary failure")
			}
			return nil
		},
		RetryPolicy: RetryPolicy{
			MaxAttempts: 3,
			BaseDelay:   5 * time.Millisecond,
			Jitter:      0.1,
		},
	}

	err := ExecuteStep(context.Background(), "test-flow", step)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryPolicyExhausted(t *testing.T) {
	attempts := 0
	step := Step{
		Name: "fail-step",
		Action: func(ctx context.Context) error {
			attempts++
			return errors.New("permanent failure")
		},
		RetryPolicy: RetryPolicy{
			MaxAttempts: 2,
			BaseDelay:   2 * time.Millisecond,
			Jitter:      0.0,
		},
	}

	err := ExecuteStep(context.Background(), "test-flow", step)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}
