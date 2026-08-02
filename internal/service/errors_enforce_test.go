package service_test

import (
	"errors"
	"fmt"
	"testing"

	"stellarbill-backend/internal/errcode"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/service"
)

// registeredSentinelErrors is the canonical list of every service-layer
// sentinel error that surfaces to API clients. Every entry must have a
// corresponding errcode.Register call in its package's init() function.
//
// ADDING A NEW ERROR WITHOUT ADDING IT HERE AND REGISTERING IT WILL FAIL CI.
//
// To add a new error code:
//   1. Define the var ErrXxx = errors.New(...) in the appropriate package.
//   2. Add errcode.Register(...) in that package's init().
//   3. Add the code constant to internal/errcode/registry.go.
//   4. Add the sentinel to the list below.
//   5. Document the new code in docs/error-codes.md.
var registeredSentinelErrors = []struct {
	name string
	err  error
	code errcode.Code
}{
	// service/errors.go
	{"ErrNotFound", service.ErrNotFound, errcode.CodeNotFound},
	{"ErrDeleted", service.ErrDeleted, errcode.CodeSubscriptionDeleted},
	{"ErrForbidden", service.ErrForbidden, errcode.CodeForbidden},
	{"ErrBillingParse", service.ErrBillingParse, errcode.CodeSubscriptionBillingParse},
	{"ErrExportInProgress", service.ErrExportInProgress, errcode.CodeExportInProgress},
	{"ErrInvalidTransition", service.ErrInvalidTransition, errcode.CodeSubscriptionInvalidTransition},
	{"ErrUnknownCurrentState", service.ErrUnknownCurrentState, errcode.CodeSubscriptionUnknownState},
	{"ErrInvalidStatus", service.ErrInvalidStatus, errcode.CodeSubscriptionInvalidStatus},

	// service/fees_service.go
	{"ErrInvalidAmount", service.ErrInvalidAmount, errcode.CodeFeeInvalidAmount},
	{"ErrInvalidTaxRate", service.ErrInvalidTaxRate, errcode.CodeFeeInvalidTaxRate},
	{"ErrInvalidParts", service.ErrInvalidParts, errcode.CodeFeeInvalidParts},

	// service/swap_service.go
	{"ErrInsufficientLiquidity", service.ErrInsufficientLiquidity, errcode.CodeSwapInsufficientLiquidity},

	// pagination/limit.go
	{"ErrInvalidLimit", pagination.ErrInvalidLimit, errcode.CodeInvalidLimit},

	// middleware/idempotency_store.go
	{"ErrRequestMismatch", middleware.ErrRequestMismatch, errcode.CodeIdempotencyRequestMismatch},
}

// TestEverySentinelErrorIsRegistered ensures that every service-level
// sentinel error added to registeredSentinelErrors has a matching entry in
// the errcode registry. This test must be updated whenever a new sentinel
// error is introduced — CI will fail otherwise.
func TestEverySentinelErrorIsRegistered(t *testing.T) {
	for _, entry := range registeredSentinelErrors {
		t.Run(entry.name, func(t *testing.T) {
			code, found := errcode.MustLookup(entry.err)
			if !found {
				t.Errorf("sentinel error %q (%v) is NOT registered in errcode — "+
					"add errcode.Register(...) in the package's init() and update registeredSentinelErrors",
					entry.name, entry.err)
				return
			}
			if code != entry.code {
				t.Errorf("sentinel error %q has code %q, want %q",
					entry.name, code, entry.code)
			}
		})
	}
}

// TestAllRegisteredCodesAreUsed ensures no stale codes remain in the
// registry without a matching sentinel error in the enforcement list.
// This catches the case where a code is added but the error definition
// is later removed.
func TestAllRegisteredCodesAreUsed(t *testing.T) {
	allCodes := errcode.AllCodes()
	if len(allCodes) == 0 {
		t.Fatal("expected non-empty code list from AllCodes()")
	}

	// Build a set of expected codes from the enforcement list.
	expected := make(map[errcode.Code]bool)
	for _, entry := range registeredSentinelErrors {
		expected[entry.code] = true
	}
	// Plus: the general-purpose codes that aren't tied to specific sentinel errors.
	expected[errcode.CodeBadRequest] = true
	expected[errcode.CodeUnauthorized] = true
	expected[errcode.CodeForbidden] = true
	expected[errcode.CodeNotFound] = true
	expected[errcode.CodeConflict] = true
	expected[errcode.CodeValidationFailed] = true
	expected[errcode.CodeUnknownField] = true
	expected[errcode.CodeInternalError] = true
	expected[errcode.CodeServiceUnavailable] = true

	for _, code := range allCodes {
		if !expected[code] {
			t.Errorf("code %q is registered in errcode but not accounted for in registeredSentinelErrors or general-purpose codes", code)
		}
	}
}

// TestAllSentinelErrorsHaveNonEmptyCode verifies every sentinel resolves
// to a non-empty code string.
func TestAllSentinelErrorsHaveNonEmptyCode(t *testing.T) {
	for _, entry := range registeredSentinelErrors {
		if entry.code == "" {
			t.Errorf("sentinel error %q has an empty code", entry.name)
		}
	}
}

// TestSentinelErrorsWrapCorrectly verifies that each sentinel can be
// resolved even when wrapped with fmt.Errorf("...: %w", sentinel).
func TestSentinelErrorsWrapCorrectly(t *testing.T) {
	type wrapCase struct {
		name     string
		sentinel error
		wantCode errcode.Code
	}
	cases := []wrapCase{
		{"ErrNotFound", service.ErrNotFound, errcode.CodeNotFound},
		{"ErrInvalidTransition", service.ErrInvalidTransition, errcode.CodeSubscriptionInvalidTransition},
		{"ErrInvalidAmount", service.ErrInvalidAmount, errcode.CodeFeeInvalidAmount},
		{"ErrInvalidLimit", pagination.ErrInvalidLimit, errcode.CodeInvalidLimit},
		{"ErrRequestMismatch", middleware.ErrRequestMismatch, errcode.CodeIdempotencyRequestMismatch},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := fmt.Errorf("wrapped: %w", tc.sentinel)
			code := errcode.Lookup(wrapped)
			if code != tc.wantCode {
				t.Errorf("wrapped %s: got code %q, want %q", tc.name, code, tc.wantCode)
			}
		})
	}
}
