package httpclient

import (
	"context"
	"math/rand"
	"net/http"
	"time"
)

// Client wraps an http.Client with retry, timeout, and circuit breaker logic.
type Client struct {
	HTTPClient     *http.Client
	Breaker        *CircuitBreaker
	MaxRetries     int
	BaseBackoff    time.Duration
	MaxBackoff     time.Duration
	RequestTimeout time.Duration
}

// NewClient creates a resilient HTTP client initialized with sensible defaults.
func NewClient() *Client {
	return &Client{
		HTTPClient:     &http.Client{},
		Breaker:        NewCircuitBreaker(5, 15*time.Second),
		MaxRetries:     3,
		BaseBackoff:    100 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
		RequestTimeout: 10 * time.Second, // Timeout per individual request
	}
}

// Do executes an HTTP request resiliently.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if !c.Breaker.Allow() {
		return nil, ErrCircuitOpen
	}

	var resp *http.Response
	var err error

	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		// Enforce request timeout per attempt
		ctx, cancel := context.WithTimeout(req.Context(), c.RequestTimeout)
		reqWithCtx := req.WithContext(ctx)

		resp, err = c.HTTPClient.Do(reqWithCtx)

		shouldRetry := false
		if err != nil {
			shouldRetry = true
		} else if resp.StatusCode >= 500 {
			shouldRetry = true
		}

		if !shouldRetry {
			cancel()
			c.Breaker.RecordSuccess()
			return resp, nil
		}

		// Ensure body is closed safely to reuse connection
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}

		if attempt < c.MaxRetries {
			backoff := calculateBackoff(attempt, c.BaseBackoff, c.MaxBackoff)
			select {
			case <-time.After(backoff):
			case <-req.Context().Done():
				cancel()
				c.Breaker.RecordFailure()
				return nil, req.Context().Err()
			}
		}
		cancel()
	}

	c.Breaker.RecordFailure()
	if err != nil {
		return nil, err
	}
	return nil, ErrMaxRetriesReached
}

// calculateBackoff implements exponential backoff with random jitter.
func calculateBackoff(attempt int, base, max time.Duration) time.Duration {
	backoff := float64(base) * float64(int(1)<<attempt)
	if backoff > float64(max) {
		backoff = float64(max)
	}
	// Jitter up to 20%
	jitter := (rand.Float64() * 0.2) * backoff
	return time.Duration(backoff + jitter)
}

