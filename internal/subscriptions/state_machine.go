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

var knownStatuses = map[string]bool{
	StatusPending:   true,
	StatusActive:    true,
	StatusPaused:    true,
	StatusCancelled: true,
	StatusExpired:   true,
}

// IsKnownStatus checks whether a status string is a recognised subscription status.
func IsKnownStatus(s string) bool {
	return knownStatuses[s]
}

// CanTransition validates state change
func CanTransition(from, to string) error {
	if from == to {
		return nil // no-op allowed
	}

	allowed, ok := allowedTransitions[from]
	if !ok {
		return fmt.Errorf("unknown current state: %s", from)
	}

	for _, a := range allowed {
		if a == to {
			return nil
		}
	}

	return fmt.Errorf("invalid transition from %s to %s", from, to)
}
