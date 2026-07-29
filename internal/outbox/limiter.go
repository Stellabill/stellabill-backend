package outbox

import (
	"context"
	"math"
	"sync"
	"time"
)

// Limiter defines a concurrency limiter interface.
type Limiter interface {
	// Acquire acquires a token from the limiter. It returns a release function
	// and an error if the context is canceled or a timeout occurs.
	Acquire(ctx context.Context) (func(error), error)
}

// LimiterConfig holds the configuration for Gradient2Limiter.
type LimiterConfig struct {
	InitialLimit float64
	MinLimit     float64
	MaxLimit     float64
	Smoothing    float64
}

// DefaultLimiterConfig returns sensible defaults for the limiter.
func DefaultLimiterConfig() LimiterConfig {
	return LimiterConfig{
		InitialLimit: 20,
		MinLimit:     1,
		MaxLimit:     1000,
		Smoothing:    0.2, // Small smoothing to allow gradual growth
	}
}

// Gradient2Limiter is an adaptive concurrency limiter loosely based on Netflix's Gradient2 algorithm.
type Gradient2Limiter struct {
	mu        sync.Mutex
	inflight  int
	limit     float64
	minLimit  float64
	maxLimit  float64
	rttNoLoad time.Duration
	smoothing float64
}

// NewGradient2Limiter creates a new Gradient2Limiter with the given config.
func NewGradient2Limiter(cfg LimiterConfig) *Gradient2Limiter {
	l := &Gradient2Limiter{
		limit:     cfg.InitialLimit,
		minLimit:  cfg.MinLimit,
		maxLimit:  cfg.MaxLimit,
		smoothing: cfg.Smoothing,
	}
	OutboxPublisherLimit.Set(l.limit)
	OutboxPublisherInflight.Set(0)
	return l
}

// Acquire blocks until a concurrency token is available or the context is done.
// Returns a release function that must be called when the operation finishes.
func (l *Gradient2Limiter) Acquire(ctx context.Context) (func(error), error) {
	for {
		l.mu.Lock()
		currentLimit := int(math.Floor(l.limit))
		if l.inflight < currentLimit {
			l.inflight++
			OutboxPublisherInflight.Set(float64(l.inflight))
			l.mu.Unlock()

			start := time.Now()
			var once sync.Once
			return func(err error) {
				once.Do(func() {
					l.release(time.Since(start), err)
				})
			}, nil
		}
		l.mu.Unlock()

		// Backoff before checking again
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
			// Retry
		}
	}
}

func (l *Gradient2Limiter) release(rtt time.Duration, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.inflight--
	OutboxPublisherInflight.Set(float64(l.inflight))

	// Under sustained timeouts or errors, decay the limit to minLimit.
	if err != nil {
		l.limit = l.limit * 0.5
		if l.limit < l.minLimit {
			l.limit = l.minLimit
		}
		OutboxPublisherLimit.Set(l.limit)
		return
	}

	if l.rttNoLoad == 0 || rtt < l.rttNoLoad {
		l.rttNoLoad = rtt
	}

	// Calculate gradient based on Netflix Gradient2 logic
	// Gradient is bounded to avoid overly drastic swings in a single step
	var gradient float64
	if rtt > 0 {
		// Using a tolerance factor (e.g. 2.0) avoids reacting to minor jitter
		adjustedRttNoLoad := time.Duration(float64(l.rttNoLoad) * 2.0)
		gradient = math.Max(0.5, math.Min(1.0, float64(adjustedRttNoLoad)/float64(rtt)))
	} else {
		gradient = 1.0
	}

	// Update limit
	l.limit = l.limit*gradient + l.smoothing

	if l.limit < l.minLimit {
		l.limit = l.minLimit
	}
	if l.limit > l.maxLimit {
		l.limit = l.maxLimit
	}

	OutboxPublisherLimit.Set(l.limit)
}
