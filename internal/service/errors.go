package service

import "errors"

var (
	// ErrNotFound is returned when the requested subscription does not exist.
	ErrNotFound = errors.New("not found")

	// ErrDeleted is returned when the subscription has been soft-deleted.
	ErrDeleted = errors.New("subscription has been deleted")

	// ErrForbidden is returned when the caller does not own the subscription.
	ErrForbidden = errors.New("forbidden")

	// ErrBillingParse is returned when the subscription's amount cannot be parsed.
	ErrBillingParse = errors.New("billing parse error")

	// ErrInvalidStatus is returned when the requested target status is unknown.
	ErrInvalidStatus = errors.New("invalid subscription status")

	// ErrInvalidTransition is returned when a requested state change is not allowed.
	ErrInvalidTransition = errors.New("invalid subscription transition")

	// ErrUnknownCurrentState is returned when persisted subscription state is outside the known graph.
	ErrUnknownCurrentState = errors.New("unknown subscription state")
)
