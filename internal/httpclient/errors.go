package httpclient

import "errors"

var (
	// ErrCircuitOpen is returned when the external service is protected by a circuit breaker that is currently open.
	ErrCircuitOpen = errors.New("circuit breaker is open")
	// ErrMaxRetriesReached is returned when the client exhausts all retry attempts.
	ErrMaxRetriesReached = errors.New("max retries reached")
)

