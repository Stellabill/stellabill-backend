package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGradient2Limiter_Acquire(t *testing.T) {
	cfg := DefaultLimiterConfig()
	cfg.InitialLimit = 5
	cfg.MinLimit = 1
	cfg.MaxLimit = 10
	cfg.Smoothing = 0.2

	limiter := NewGradient2Limiter(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var releases []func(error)

	// Acquire up to initial limit
	for i := 0; i < 5; i++ {
		rel, err := limiter.Acquire(ctx)
		if err != nil {
			t.Fatalf("unexpected error on acquire %d: %v", i, err)
		}
		releases = append(releases, rel)
	}

	// Next acquire should block and then fail due to context timeout
	_, err := limiter.Acquire(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	// Release all
	for _, rel := range releases {
		rel(nil)
	}

	// Now we can acquire again
	rel, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatalf("unexpected error after release: %v", err)
	}
	rel(nil)
}

func TestGradient2Limiter_ConvergenceOnTimeouts(t *testing.T) {
	cfg := DefaultLimiterConfig()
	cfg.InitialLimit = 20
	cfg.MinLimit = 1
	cfg.MaxLimit = 50

	limiter := NewGradient2Limiter(cfg)

	// Simulate sustained timeouts
	for i := 0; i < 10; i++ {
		rel, err := limiter.Acquire(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Simulate timeout error
		rel(errors.New("timeout"))
	}

	limiter.mu.Lock()
	limit := limiter.limit
	limiter.mu.Unlock()

	// Limit should converge to MinLimit (1)
	if limit > 1.5 {
		t.Fatalf("expected limit to converge near 1, got %f", limit)
	}
}

func TestGradient2Limiter_IncreaseOnSuccess(t *testing.T) {
	cfg := DefaultLimiterConfig()
	cfg.InitialLimit = 5
	cfg.MinLimit = 1
	cfg.MaxLimit = 50

	limiter := NewGradient2Limiter(cfg)

	// Establish RttNoLoad
	rel, _ := limiter.Acquire(context.Background())
	time.Sleep(5 * time.Millisecond)
	rel(nil)

	limiter.mu.Lock()
	initialLimit := limiter.limit
	limiter.mu.Unlock()

	// Simulate success with good RTT to increase limit
	for i := 0; i < 10; i++ {
		rel, _ := limiter.Acquire(context.Background())
		time.Sleep(2 * time.Millisecond) // Faster than NoLoad (which is fine, will update NoLoad)
		rel(nil)
	}

	limiter.mu.Lock()
	limit := limiter.limit
	limiter.mu.Unlock()

	if limit <= initialLimit {
		t.Fatalf("expected limit to increase, got %f (initial was %f)", limit, initialLimit)
	}
}
