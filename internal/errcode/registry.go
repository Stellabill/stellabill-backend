package errcode

import (
	"fmt"
)

// Code is a stable, structured error identifier for API responses.
type Code string

const (
	// Client errors
	CodeBadRequest       Code = "client/bad-request"
	CodeValidationFailed Code = "client/validation-failed"
	CodeUnauthorized     Code = "client/unauthorized"
	CodeForbidden        Code = "client/forbidden"
	CodeNotFound         Code = "client/not-found"
	CodeConflict         Code = "client/conflict"
	CodeUnknownField     Code = "client/unknown-field"

	// Subscription errors
	CodeSubscriptionNotFound          Code = "subscription/not-found"
	CodeSubscriptionDeleted           Code = "subscription/deleted"
	CodeSubscriptionForbidden         Code = "subscription/forbidden"
	CodeSubscriptionInvalidTransition Code = "subscription/invalid-state-transition"
	CodeSubscriptionUnknownState      Code = "subscription/unknown-state"
	CodeSubscriptionInvalidStatus     Code = "subscription/invalid-status"
	CodeSubscriptionBillingParse      Code = "subscription/billing-parse-error"

	// Export errors
	CodeExportInProgress Code = "export/in-progress"

	// Fee errors
	CodeFeeInvalidAmount  Code = "fee/invalid-amount"
	CodeFeeInvalidTaxRate Code = "fee/invalid-tax-rate"
	CodeFeeInvalidParts   Code = "fee/invalid-parts"

	// Pagination errors
	CodeInvalidLimit Code = "pagination/invalid-limit"

	// Idempotency errors
	CodeIdempotencyRequestMismatch Code = "idempotency/request-mismatch"

	// Swap errors
	CodeSwapInsufficientLiquidity Code = "swap/insufficient-liquidity"

	// System errors
	CodeInternalError      Code = "system/internal-error"
	CodeServiceUnavailable Code = "system/service-unavailable"
)

// entry maps an error to a code via a matcher function.
type entry struct {
	matcher func(error) bool
	code    Code
}

var registry = make(map[Code]struct{})
var matchers []entry

// Register adds a matcher-to-code mapping. Panics if the code is
// already registered or if the matcher is nil.
func Register(matcher func(error) bool, code Code) {
	if matcher == nil {
		panic("errcode: nil matcher")
	}
	if _, exists := registry[code]; exists {
		panic(fmt.Sprintf("errcode: code %q is already registered", code))
	}
	registry[code] = struct{}{}
	matchers = append(matchers, entry{matcher: matcher, code: code})
}

// Lookup returns the registered code for err. If no matcher matches,
// CodeInternalError is returned.
func Lookup(err error) Code {
	if err == nil {
		return ""
	}
	for _, e := range matchers {
		if e.matcher(err) {
			return e.code
		}
	}
	return CodeInternalError
}

// MustLookup returns the registered code for err along with a found flag.
func MustLookup(err error) (Code, bool) {
	if err == nil {
		return "", true
	}
	for _, e := range matchers {
		if e.matcher(err) {
			return e.code, true
		}
	}
	return "", false
}

// AllCodes returns every registered code.
func AllCodes() []Code {
	codes := make([]Code, 0, len(matchers))
	for _, e := range matchers {
		codes = append(codes, e.code)
	}
	return codes
}

// IsRegistered reports whether the given code has been registered.
func IsRegistered(code Code) bool {
	_, exists := registry[code]
	return exists
}

// Count returns the number of registered error codes.
func Count() int {
	return len(matchers)
}
