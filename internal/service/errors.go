package service

import (
	"errors"

	"stellarbill-backend/internal/errcode"
)

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

func init() {
	errcode.Register(func(err error) bool { return errors.Is(err, ErrNotFound) }, errcode.CodeNotFound)
	errcode.Register(func(err error) bool { return errors.Is(err, ErrDeleted) }, errcode.CodeSubscriptionDeleted)
	errcode.Register(func(err error) bool { return errors.Is(err, ErrForbidden) }, errcode.CodeForbidden)
	errcode.Register(func(err error) bool { return errors.Is(err, ErrBillingParse) }, errcode.CodeSubscriptionBillingParse)
	errcode.Register(func(err error) bool { return errors.Is(err, ErrExportInProgress) }, errcode.CodeExportInProgress)
	errcode.Register(func(err error) bool { return errors.Is(err, ErrInvalidTransition) }, errcode.CodeSubscriptionInvalidTransition)
	errcode.Register(func(err error) bool { return errors.Is(err, ErrUnknownCurrentState) }, errcode.CodeSubscriptionUnknownState)
	errcode.Register(func(err error) bool { return errors.Is(err, ErrInvalidStatus) }, errcode.CodeSubscriptionInvalidStatus)
}
