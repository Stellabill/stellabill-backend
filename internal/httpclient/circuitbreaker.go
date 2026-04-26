package httpclient

import (
	"sync"
	"time"

	"go.uber.org/zap"
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
	openedAt       time.Time
	probeStartedAt time.Time
	host           string
	logger       *zap.Logger
}

// NewCircuitBreaker initializes a new CircuitBreaker.
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration, host string, logger *zap.Logger) *CircuitBreaker {
	if logger == nil {
		logger = zap.NewNop()
	}
	cb := &CircuitBreaker{
		state:        StateClosed,
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		host:         host,
		logger:       logger,
	}
	HTTPClientCircuitState.WithLabelValues(host).Set(float64(StateClosed))
	return cb
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
		// Circuit is open. If the reset timeout has elapsed, allow exactly one probe request (Half-Open)
		if time.Since(cb.openedAt) > cb.resetTimeout {
			cb.state = StateHalfOpen
			cb.probeStartedAt = time.Now()
			HTTPClientCircuitState.WithLabelValues(cb.host).Set(float64(StateHalfOpen))
			cb.logger.Info("Circuit breaker half-opened, allowing probe request", zap.String("host", cb.host))
			return true
		}
		// Reset timeout has not elapsed; reject request
		return false
	case StateHalfOpen:
		// A probe request is currently in-flight. Reject others unless the probe has hung past the timeout threshold.
		if time.Since(cb.probeStartedAt) > cb.resetTimeout {
			cb.probeStartedAt = time.Now()
			cb.logger.Warn("Circuit breaker previous probe hung, allowing new probe request", zap.String("host", cb.host))
			return true
		}
		return false
	case StateClosed:
		// Normal operation; allow all requests
		return true
	}
	return true
}

// RecordSuccess records a successful request, resetting failures.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// If a probe request succeeded, the service has recovered; close the circuit.
	if cb.state == StateHalfOpen || cb.state == StateOpen {
		cb.state = StateClosed
		cb.failures = 0
		HTTPClientCircuitState.WithLabelValues(cb.host).Set(float64(StateClosed))
		cb.logger.Info("Circuit breaker closed", zap.String("host", cb.host))
	} else if cb.failures > 0 {
		// Reset transient failures during normal operation
		cb.failures = 0
	}
}

// RecordFailure records a failed request, transitioning state if threshold met.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// If in Half-Open state, the probe failed; immediately re-open the circuit.
	if cb.state == StateHalfOpen {
		cb.state = StateOpen
		cb.openedAt = time.Now()
		HTTPClientCircuitState.WithLabelValues(cb.host).Set(float64(StateOpen))
		cb.logger.Warn("Circuit breaker opened (probe failed)", zap.String("host", cb.host))
		return
	}

	// Normal operation: track failures until threshold is reached.
	cb.failures++
	if cb.failures >= cb.maxFailures {
		cb.state = StateOpen
		cb.openedAt = time.Now()
		HTTPClientCircuitState.WithLabelValues(cb.host).Set(float64(StateOpen))
		cb.logger.Warn("Circuit breaker opened (threshold reached)", zap.String("host", cb.host), zap.Int("failures", cb.failures))
	}
}
