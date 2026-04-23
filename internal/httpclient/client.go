package httpclient

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// Client wraps an http.Client with retry, timeout, and circuit breaker logic.
// It also enforces idempotency requirements for retries to prevent duplicate side effects.
type Client struct {
	HTTPClient         *http.Client
	Breaker            *CircuitBreaker
	MaxRetries         int
	BaseBackoff        time.Duration
	MaxBackoff         time.Duration
	RequestTimeout     time.Duration
	Host               string
	logger             *zap.Logger
	RetryNonIdempotent bool // Whether to retry non-idempotent methods without an Idempotency-Key
}

// NewClient creates a resilient HTTP client initialized with sensible defaults.
func NewClient(host string, logger *zap.Logger) *Client {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Client{
		HTTPClient:     &http.Client{},
		Breaker:        NewCircuitBreaker(5, 15*time.Second, host, logger),
		MaxRetries:     3,
		BaseBackoff:    100 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
		RequestTimeout: 10 * time.Second, // Timeout per individual request
		Host:           host,
		logger:         logger,
	}
}

// isIdempotent returns true if the HTTP method is considered idempotent.
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// bodyCloser wraps an io.ReadCloser and calls a cancel function upon closing.
type bodyCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *bodyCloser) Close() error {
	defer b.cancel()
	return b.ReadCloser.Close()
}

// parseRetryAfter attempts to parse a Retry-After header as seconds.
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(header); err == nil {
		return time.Duration(seconds) * time.Second
	}
	// Note: RFC HTTP-date parsing omitted for simplicity, relying on integer seconds.
	return 0
}

// Do executes an HTTP request resiliently.
//
// Retry vs Fail Fast:
// - Retries are attempted for network errors, timeouts, and 5xx server errors.
// - Fail fast occurs when the circuit breaker is open (too many recent failures).
// - To prevent duplicate side effects, non-idempotent methods (e.g., POST, PATCH)
//   will NOT be retried unless an "Idempotency-Key" header is provided, or
//   RetryNonIdempotent is explicitly set to true.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if !c.Breaker.Allow() {
		return nil, ErrCircuitOpen
	}

	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		// Enforce request timeout per attempt
		ctx, cancel := context.WithTimeout(req.Context(), c.RequestTimeout)
		reqWithCtx := req.WithContext(ctx)

		resp, err := c.HTTPClient.Do(reqWithCtx)

		shouldRetry, retryAfter := c.evaluateRetryPolicy(req, resp, err)

		// Terminal case: success, un-retryable error, or max retries reached
		if !shouldRetry || attempt >= c.MaxRetries {
			return c.handleTerminalResponse(req, resp, err, attempt, cancel)
		}

		// Drain up to 4KB of the body to allow TCP connection reuse before closing
		if resp != nil && resp.Body != nil {
			io.CopyN(io.Discard, resp.Body, 4096)
			resp.Body.Close()
		}

		// Attempt backoff
		if backoffErr := c.sleepForBackoff(req.Context(), req.Method, attempt, retryAfter, cancel); backoffErr != nil {
			return nil, backoffErr
		}
	}

	// This point should be unreachable due to attempt >= c.MaxRetries check above
	return nil, fmt.Errorf("max retries reached")
}

// evaluateRetryPolicy determines if the request should be retried and returns any Retry-After duration.
func (c *Client) evaluateRetryPolicy(req *http.Request, resp *http.Response, err error) (bool, time.Duration) {
	shouldRetry := false
	var retryAfter time.Duration

	if err != nil {
		shouldRetry = true
	} else if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		shouldRetry = true
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		}
	}

	// Enforce idempotency requirements for retries
	if shouldRetry {
		method := req.Method
		if method == "" {
			method = http.MethodGet
		}
		canRetry := isIdempotent(method) || req.Header.Get("Idempotency-Key") != "" || c.RetryNonIdempotent
		if !canRetry {
			shouldRetry = false
		}
	}

	return shouldRetry, retryAfter
}

// handleTerminalResponse records the final circuit breaker state, metrics, and manages context cancellation.
func (c *Client) handleTerminalResponse(req *http.Request, resp *http.Response, err error, attempt int, cancel context.CancelFunc) (*http.Response, error) {
	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		cancel()
		c.Breaker.RecordFailure()

		reason := "non_2xx"
		if err != nil {
			reason = "error"
		} else if attempt >= c.MaxRetries && attempt > 0 {
			reason = "max_retries_reached"
		}
		HTTPClientFailuresTotal.WithLabelValues(c.Host, req.Method, reason).Inc()

		if err != nil {
			c.logger.Error("HTTP request failed permanently", zap.String("host", c.Host), zap.String("method", req.Method), zap.Int("attempt", attempt+1), zap.Error(err))
			err = fmt.Errorf("request failed after %d attempts: %w", attempt+1, err)
		} else {
			c.logger.Error("HTTP request returned server error", zap.String("host", c.Host), zap.String("method", req.Method), zap.Int("status_code", resp.StatusCode), zap.Int("attempt", attempt+1))
		}
	} else {
		c.Breaker.RecordSuccess()
		if resp != nil && resp.Body != nil {
			resp.Body = &bodyCloser{ReadCloser: resp.Body, cancel: cancel}
		} else {
			cancel()
		}
	}
	return resp, err
}

// sleepForBackoff calculates the backoff duration and sleeps, respecting context cancellation and Retry-After headers.
func (c *Client) sleepForBackoff(ctx context.Context, method string, attempt int, retryAfter time.Duration, cancel context.CancelFunc) error {
	HTTPClientRetriesTotal.WithLabelValues(c.Host, method).Inc()
	backoff := calculateBackoff(attempt, c.BaseBackoff, c.MaxBackoff)
	
	// Override with Retry-After if present and valid
	if retryAfter > 0 {
		if retryAfter > c.MaxBackoff {
			backoff = c.MaxBackoff // Cap it to prevent excessive waits
		} else {
			backoff = retryAfter
		}
	}

	c.logger.Warn("Retrying HTTP request", zap.String("host", c.Host), zap.String("method", method), zap.Int("attempt", attempt+1), zap.Duration("backoff", backoff))
	select {
	case <-time.After(backoff):
		return nil
	case <-ctx.Done():
		cancel()
		c.Breaker.RecordFailure()
		HTTPClientFailuresTotal.WithLabelValues(c.Host, method, "timeout").Inc()
		c.logger.Error("HTTP request timed out during backoff", zap.String("host", c.Host), zap.String("method", method), zap.Error(ctx.Err()))
		return fmt.Errorf("request timed out: %w", ctx.Err())
	}
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
