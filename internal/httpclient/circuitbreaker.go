package httpclient

import (
	"sync"
	"time"
)

// State represents the state of the circuit breaker.
type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

// CircuitBreaker implements a simple state machine to prevent cascading failures.
type CircuitBreaker struct {
	mu           sync.RWMutex
	state        State
	failures     int
	maxFailures  int
	resetTimeout time.Duration
	openedAt     time.Time
}

// NewCircuitBreaker initializes a new CircuitBreaker.
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:        StateClosed,
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
	}
}

// State returns the current State.
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.state == StateOpen {
		if time.Since(cb.openedAt) > cb.resetTimeout {
			return StateHalfOpen
		}
	}
	return cb.state
}

// Allow determines if a request is allowed to proceed based on the circuit state.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateOpen:
		if time.Since(cb.openedAt) > cb.resetTimeout {
			// Transition to HalfOpen to allow a single probe request
			cb.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		// Only one probe request allowed at a time. Others fail fast.
		return false
	case StateClosed:
		return true
	}
	return true
}

// RecordSuccess records a successful request, resetting failures.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateHalfOpen || cb.state == StateOpen {
		cb.state = StateClosed
		cb.failures = 0
	} else if cb.failures > 0 {
		cb.failures = 0
	}
}

// RecordFailure records a failed request, transitioning state if threshold met.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateHalfOpen {
		cb.state = StateOpen
		cb.openedAt = time.Now()
		return
	}

	cb.failures++
	if cb.failures >= cb.maxFailures {
		cb.state = StateOpen
		cb.openedAt = time.Now()
	}
}
