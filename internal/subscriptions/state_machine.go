package subscriptions

import "fmt"

// Subscription statuses
const (
	StatusPending   = "pending"
	StatusActive    = "active"
	StatusPaused    = "paused"
	StatusCancelled = "cancelled"
	StatusExpired   = "expired"
)

// allowedTransitions defines valid state transitions
var allowedTransitions = map[string][]string{
	StatusPending:   {StatusActive, StatusCancelled},
	StatusActive:    {StatusPaused, StatusCancelled, StatusExpired},
	StatusPaused:    {StatusActive, StatusCancelled},
	StatusCancelled: {},
	StatusExpired:   {},
}

// IsKnownStatus reports whether status is part of the supported subscription state graph.
func IsKnownStatus(status string) bool {
	_, ok := allowedTransitions[status]
	return ok
}

// CanTransition validates state change
func CanTransition(from, to string) error {
	allowed, ok := allowedTransitions[from]
	if !ok {
		return fmt.Errorf("unknown current state: %s", from)
	}

	if from == to {
		return nil // no-op allowed
	}

	for _, a := range allowed {
		if a == to {
			return nil
		}
	}

	return fmt.Errorf("invalid transition from %s to %s", from, to)
}
