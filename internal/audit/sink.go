package audit

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// FailurePolicy controls how critical-path callers behave when the sink is degraded.
type FailurePolicy int

const (
	// FailOpen allows the operation to proceed even if the audit write fails.
	FailOpen FailurePolicy = iota
	// FailClosed blocks the operation when the audit write cannot be confirmed.
	FailClosed
)

// ErrSinkUnavailable is returned by a RetryingSink when all retry attempts are exhausted.
var ErrSinkUnavailable = errors.New("audit sink unavailable after retries")

// FileSink appends JSONL audit entries to a file path.
type FileSink struct {
	mu   sync.Mutex
	path string
}

// NewFileSink returns a sink that writes to the provided path (default: audit.log).
func NewFileSink(path string) *FileSink {
	if path == "" {
		path = "audit.log"
	}
	return &FileSink{path: path}
}

func (s *FileSink) WriteEvent(e AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	encoded, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(encoded, '\n'))
	return err
}

// RetryingSink wraps an inner Sink and retries failed writes with bounded backoff.
// It also tracks failure metrics and enforces a FailurePolicy for callers.
type RetryingSink struct {
	inner      Sink
	maxRetries int
	baseDelay  time.Duration
	policy     FailurePolicy

	// metrics (read with atomic)
	totalWrites   atomic.Int64
	totalFailures atomic.Int64
	totalRetries  atomic.Int64
}

// NewRetryingSink wraps inner with retry logic. maxRetries must be >= 1.
// baseDelay is the initial backoff (doubles each attempt). policy controls
// whether a final failure is surfaced to the caller (FailClosed) or swallowed (FailOpen).
func NewRetryingSink(inner Sink, maxRetries int, baseDelay time.Duration, policy FailurePolicy) *RetryingSink {
	if maxRetries < 1 {
		maxRetries = 1
	}
	return &RetryingSink{inner: inner, maxRetries: maxRetries, baseDelay: baseDelay, policy: policy}
}

// WriteEntry attempts to write to the inner sink, retrying up to maxRetries times
// with exponential backoff. On exhaustion, behaviour depends on the FailurePolicy.
func (s *RetryingSink) WriteEntry(e Entry) error {
	s.totalWrites.Add(1)
	delay := s.baseDelay
	var lastErr error
	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		if attempt > 0 {
			s.totalRetries.Add(1)
			time.Sleep(delay)
			delay *= 2
		}
		if err := s.inner.WriteEntry(e); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	_ = lastErr
	s.totalFailures.Add(1)
	if s.policy == FailClosed {
		return ErrSinkUnavailable
	}
	return nil // FailOpen: swallow the error
}

// Metrics returns a snapshot of write/failure/retry counters.
func (s *RetryingSink) Metrics() (writes, failures, retries int64) {
	return s.totalWrites.Load(), s.totalFailures.Load(), s.totalRetries.Load()
}

// MemorySink keeps audit entries in-memory, intended for tests.
type MemorySink struct {
	mu      sync.Mutex
	entries []AuditEvent
}

// WriteEvent satisfies the Sink interface.
func (s *MemorySink) WriteEvent(e AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.FailAfter > 0 && s.writes >= s.FailAfter {
		return errors.New("simulated sink failure")
	}
	s.entries = append(s.entries, e)
	s.writes++
	return nil
}

// Entries returns a copy of stored entries.
func (s *MemorySink) Entries() []AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AuditEvent, len(s.entries))
	copy(out, s.entries)
	return out
}
