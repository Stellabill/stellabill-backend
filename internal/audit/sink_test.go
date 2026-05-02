package audit

import (
	"context"
	"errors"
	"testing"
	"time"
)

// errorSink always returns an error.
type errorSink struct{}

func (e *errorSink) WriteEntry(_ Entry) error { return errors.New("sink down") }

func TestRetryingSink_SucceedsOnFirstAttempt(t *testing.T) {
	inner := &MemorySink{}
	s := NewRetryingSink(inner, 3, time.Millisecond, FailClosed)
	logger := NewLogger("secret", s)

	if _, err := logger.Log(context.Background(), "actor", "action", "target", "ok", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inner.Entries()) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(inner.Entries()))
	}
	writes, failures, retries := s.Metrics()
	if writes != 1 || failures != 0 || retries != 0 {
		t.Fatalf("unexpected metrics: writes=%d failures=%d retries=%d", writes, failures, retries)
	}
}

func TestRetryingSink_RetriesOnFailure(t *testing.T) {
	// Fail the first 2 writes, succeed on the 3rd.
	callCount := 0
	var successSink Sink = &callCountSink{
		fn: func(e Entry) error {
			callCount++
			if callCount < 3 {
				return errors.New("transient")
			}
			return nil
		},
	}
	s := NewRetryingSink(successSink, 3, time.Millisecond, FailClosed)
	logger := NewLogger("secret", s)

	if _, err := logger.Log(context.Background(), "actor", "action", "target", "ok", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, retries := s.Metrics()
	if retries < 2 {
		t.Fatalf("expected at least 2 retries, got %d", retries)
	}
}

func TestRetryingSink_FailClosedReturnsError(t *testing.T) {
	s := NewRetryingSink(&errorSink{}, 2, time.Millisecond, FailClosed)
	logger := NewLogger("secret", s)

	_, err := logger.Log(context.Background(), "actor", "action", "target", "ok", nil)
	if !errors.Is(err, ErrSinkUnavailable) {
		t.Fatalf("expected ErrSinkUnavailable, got %v", err)
	}
	_, failures, _ := s.Metrics()
	if failures != 1 {
		t.Fatalf("expected 1 failure recorded, got %d", failures)
	}
}

func TestRetryingSink_FailOpenSwallowsError(t *testing.T) {
	s := NewRetryingSink(&errorSink{}, 2, time.Millisecond, FailOpen)
	logger := NewLogger("secret", s)

	if _, err := logger.Log(context.Background(), "actor", "action", "target", "ok", nil); err != nil {
		t.Fatalf("FailOpen should not surface error, got %v", err)
	}
	_, failures, _ := s.Metrics()
	if failures != 1 {
		t.Fatalf("expected 1 failure recorded, got %d", failures)
	}
}

func TestRetryingSink_MetricsAccumulate(t *testing.T) {
	inner := &MemorySink{}
	s := NewRetryingSink(inner, 1, time.Millisecond, FailOpen)
	logger := NewLogger("secret", s)

	for i := 0; i < 5; i++ {
		logger.Log(context.Background(), "a", "b", "c", "ok", nil) //nolint:errcheck
	}
	writes, _, _ := s.Metrics()
	if writes != 5 {
		t.Fatalf("expected 5 writes, got %d", writes)
	}
}

func TestMemorySink_FailAfter(t *testing.T) {
	inner := &MemorySink{FailAfter: 2}
	s := NewRetryingSink(inner, 0, time.Millisecond, FailClosed)
	logger := NewLogger("secret", s)

	// First two succeed.
	for i := 0; i < 2; i++ {
		if _, err := logger.Log(context.Background(), "a", "b", "c", "ok", nil); err != nil {
			t.Fatalf("write %d should succeed: %v", i, err)
		}
	}
	// Third should fail (FailClosed, maxRetries=0).
	if _, err := logger.Log(context.Background(), "a", "b", "c", "ok", nil); !errors.Is(err, ErrSinkUnavailable) {
		t.Fatalf("expected ErrSinkUnavailable after FailAfter, got %v", err)
	}
}

// callCountSink is a test helper that delegates to a function.
type callCountSink struct{ fn func(Entry) error }

func (c *callCountSink) WriteEntry(e Entry) error { return c.fn(e) }
