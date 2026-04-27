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
)

