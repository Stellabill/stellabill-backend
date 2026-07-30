package errcode

import (
	"errors"
	"testing"
)

func TestRegisterAndLookup(t *testing.T) {
	called := false
	Register(func(err error) bool {
		called = true
		return errors.Is(err, sentinelError("test registered error"))
	}, CodeBadRequest)

	err := sentinelError("test registered error")
	code := Lookup(err)
	if code != CodeBadRequest {
		t.Errorf("expected %q, got %q", CodeBadRequest, code)
	}
	if !called {
		t.Error("matcher was not called")
	}
}

func TestLookupNilError(t *testing.T) {
	code := Lookup(nil)
	if code != "" {
		t.Errorf("expected empty code for nil error, got %q", code)
	}
}

func TestLookupUnregisteredError(t *testing.T) {
	err := errors.New("completely unknown error")
	code := Lookup(err)
	if code != CodeInternalError {
		t.Errorf("expected %q for unregistered error, got %q", CodeInternalError, code)
	}
}

func TestMustLookupFound(t *testing.T) {
	Register(func(err error) bool {
		return errors.Is(err, sentinelError("must lookup test"))
	}, CodeNotFound)

	err := sentinelError("must lookup test")
	code, found := MustLookup(err)
	if !found {
		t.Error("expected MustLookup to find the error")
	}
	if code != CodeNotFound {
		t.Errorf("expected %q, got %q", CodeNotFound, code)
	}
}

func TestMustLookupNotFound(t *testing.T) {
	code, found := MustLookup(errors.New("not registered"))
	if found {
		t.Error("expected MustLookup to not find the error")
	}
	if code != "" {
		t.Errorf("expected empty code when not found, got %q", code)
	}
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	Register(func(err error) bool { return false }, CodeConflict)
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for duplicate registration")
		}
	}()
	Register(func(err error) bool { return false }, CodeConflict)
}

func TestNilMatcherPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for nil matcher")
		}
	}()
	Register(nil, CodeBadRequest)
}

func TestAllCodes(t *testing.T) {
	codes := AllCodes()
	if len(codes) == 0 {
		t.Error("expected non-empty code list")
	}
	for _, c := range codes {
		if c == "" {
			t.Error("found empty code in AllCodes")
		}
	}
}

func TestIsRegistered(t *testing.T) {
	Register(func(err error) bool { return false }, CodeUnknownField)
	if !IsRegistered(CodeUnknownField) {
		t.Error("expected CodeUnknownField to be registered")
	}
	if IsRegistered(Code("nonexistent/example")) {
		t.Error("expected nonexistent code to not be registered")
	}
}

func TestCount(t *testing.T) {
	initial := Count()
	Register(func(err error) bool { return false }, CodeServiceUnavailable)
	if Count() != initial+1 {
		t.Errorf("expected count to increase by 1, got %d", Count())
	}
}

func TestAllRegisteredCodesAreUnique(t *testing.T) {
	codes := AllCodes()
	seen := make(map[Code]int)
	for _, c := range codes {
		seen[c]++
	}
	for code, count := range seen {
		if count > 1 {
			t.Errorf("code %q registered %d times", code, count)
		}
	}
}

func TestCodeStringValues(t *testing.T) {
	cases := []struct {
		code Code
		want string
	}{
		{CodeBadRequest, "client/bad-request"},
		{CodeValidationFailed, "client/validation-failed"},
		{CodeUnauthorized, "client/unauthorized"},
		{CodeForbidden, "client/forbidden"},
		{CodeNotFound, "client/not-found"},
		{CodeConflict, "client/conflict"},
		{CodeUnknownField, "client/unknown-field"},
		{CodeSubscriptionNotFound, "subscription/not-found"},
		{CodeSubscriptionDeleted, "subscription/deleted"},
		{CodeSubscriptionForbidden, "subscription/forbidden"},
		{CodeSubscriptionInvalidTransition, "subscription/invalid-state-transition"},
		{CodeSubscriptionUnknownState, "subscription/unknown-state"},
		{CodeSubscriptionInvalidStatus, "subscription/invalid-status"},
		{CodeSubscriptionBillingParse, "subscription/billing-parse-error"},
		{CodeExportInProgress, "export/in-progress"},
		{CodeSwapInsufficientLiquidity, "swap/insufficient-liquidity"},
		{CodeInternalError, "system/internal-error"},
		{CodeServiceUnavailable, "system/service-unavailable"},
	}
	for _, tc := range cases {
		if string(tc.code) != tc.want {
			t.Errorf("code %q = %q, want %q", tc.code, string(tc.code), tc.want)
		}
	}
}

type sentinelError string

func (e sentinelError) Error() string { return string(e) }
