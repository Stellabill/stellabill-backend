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

	// ErrExportInProgress is returned when an export is already in progress for this tenant.
	ErrExportInProgress = errors.New("export already in progress for this tenant")

	// ErrInvalidTransition is returned when a subscription status transition is not allowed.
	ErrInvalidTransition = errors.New("invalid status transition")

	// ErrUnknownCurrentState is returned when the current subscription status is not a known value.
	ErrUnknownCurrentState = errors.New("unknown current state")

	// ErrInvalidStatus is returned when the target status is not a known subscription status.
	ErrInvalidStatus = errors.New("invalid status")
)
