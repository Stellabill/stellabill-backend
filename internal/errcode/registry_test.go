package errcode

import (
	"sort"
	"sync"
	"testing"
)

func TestNewRegistryIsEmpty(t *testing.T) {
	r := New()
	if r.Len() != 0 {
		t.Fatalf("expected empty registry, got %d codes", r.Len())
	}
}

func TestRegisterAndLookup(t *testing.T) {
	r := New()
	r.Register("test/ok", "test message", 200)

	entry, ok := r.Lookup("test/ok")
	if !ok {
		t.Fatal("expected code to be found")
	}
	if entry.Code != "test/ok" {
		t.Errorf("expected code test/ok, got %s", entry.Code)
	}
	if entry.Message != "test message" {
		t.Errorf("expected message 'test message', got %q", entry.Message)
	}
	if entry.HTTPStatus != 200 {
		t.Errorf("expected HTTP status 200, got %d", entry.HTTPStatus)
	}
}

func TestLookupNotFound(t *testing.T) {
	r := New()
	_, ok := r.Lookup("nonexistent/code")
	if ok {
		t.Fatal("expected lookup to return false for unregistered code")
	}
}

func TestRegisterEmptyCodePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for empty code")
		}
	}()
	r := New()
	r.Register("", "bad", 400)
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for duplicate code")
		}
	}()
	r := New()
	r.Register("dup/code", "first", 400)
	r.Register("dup/code", "second", 400)
}

func TestCodesReturnsSortedSlice(t *testing.T) {
	r := New()
	r.Register("z/code", "z", 400)
	r.Register("a/code", "a", 400)
	r.Register("m/code", "m", 400)

	codes := r.Codes()
	if len(codes) != 3 {
		t.Fatalf("expected 3 codes, got %d", len(codes))
	}
	if !sort.StringsAreSorted([]string{string(codes[0]), string(codes[1]), string(codes[2])}) {
		t.Errorf("codes not sorted: %v", codes)
	}
}

func TestLen(t *testing.T) {
	r := New()
	if r.Len() != 0 {
		t.Fatalf("expected 0, got %d", r.Len())
	}
	r.Register("a/1", "a", 400)
	if r.Len() != 1 {
		t.Fatalf("expected 1, got %d", r.Len())
	}
	r.Register("b/2", "b", 500)
	if r.Len() != 2 {
		t.Fatalf("expected 2, got %d", r.Len())
	}
}

func TestAllReturnsSortedEntries(t *testing.T) {
	r := New()
	r.Register("z/late", "z", 400)
	r.Register("a/first", "a", 400)

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
	if all[0].Code != "a/first" {
		t.Errorf("expected first entry a/first, got %s", all[0].Code)
	}
	if all[1].Code != "z/late" {
		t.Errorf("expected second entry z/late, got %s", all[1].Code)
	}
}

func TestConcurrentReads(t *testing.T) {
	r := New()
	r.Register("concurrent/test", "test", 400)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok := r.Lookup("concurrent/test")
			if !ok {
				t.Error("expected code to be found")
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Default registry tests
// ---------------------------------------------------------------------------

func TestDefaultRegistryIsPopulated(t *testing.T) {
	if Default.Len() == 0 {
		t.Fatal("default registry should not be empty after init")
	}
}

func TestAllExpectedDomainCodesExist(t *testing.T) {
	expected := []Code{
		// Subscription
		CodeSubscriptionNotFound,
		CodeSubscriptionSoftDeleted,
		CodeSubscriptionForbidden,
		CodeSubscriptionInvalidStatus,
		CodeSubscriptionInvalidTransition,
		CodeSubscriptionUnknownState,
		// Billing
		CodeBillingParseError,
		// Statement
		CodeStatementNotFound,
		CodeStatementForbidden,
		// Plan
		CodePlanNotFound,
		// Swap
		CodeSwapInsufficientLiquidity,
		CodeSwapInvalidAmount,
		// Export
		CodeExportInProgress,
		// Webhook
		CodeWebhookInvalidPayload,
		CodeWebhookUnknownEventType,
		CodeWebhookMissingField,
		// Auth
		CodeAuthMissing,
		CodeAuthInvalid,
		CodeAuthForbidden,
		CodeAuthInsufficientPerm,
		// Validation
		CodeValidationFailed,
		CodeValidationUnknownField,
		// Client generic
		CodeBadRequest,
		CodeNotFound,
		CodeConflict,
		CodeRateLimited,
		CodePayloadTooLarge,
		// Server generic
		CodeInternalError,
		CodeServiceUnavailable,
	}

	for _, code := range expected {
		entry, ok := Default.Lookup(code)
		if !ok {
			t.Errorf("expected code %q to be registered", code)
			continue
		}
		if entry.Code != code {
			t.Errorf("expected entry code %q, got %q", code, entry.Code)
		}
		if entry.Message == "" {
			t.Errorf("code %q has empty message", code)
		}
		if entry.HTTPStatus < 100 || entry.HTTPStatus > 599 {
			t.Errorf("code %q has invalid HTTP status %d", code, entry.HTTPStatus)
		}
	}
}

func TestDefaultCodesAreUnique(t *testing.T) {
	codes := Default.Codes()
	seen := make(map[Code]bool, len(codes))
	for _, c := range codes {
		if seen[c] {
			t.Errorf("duplicate code in default registry: %s", c)
		}
		seen[c] = true
	}
}

func TestDefaultCodesHaveConsistentFormat(t *testing.T) {
	for _, code := range Default.Codes() {
		s := string(code)
		if len(s) == 0 {
			t.Error("empty code in default registry")
			continue
		}
		// Must contain exactly one slash separating domain from operation
		slashCount := 0
		for _, ch := range s {
			if ch == '/' {
				slashCount++
			}
		}
		if slashCount != 1 {
			t.Errorf("code %q does not follow <domain>/<operation> format (found %d slashes)", code, slashCount)
		}
	}
}

func TestDefaultCodesHaveReasonableHTTPStatuses(t *testing.T) {
	for _, entry := range Default.All() {
		status := entry.HTTPStatus
		if status < 400 || status > 599 {
			t.Errorf("code %q has non-error HTTP status %d; error codes should be 4xx or 5xx", entry.Code, status)
		}
	}
}

func TestSubscriptionCodesHave4xxStatuses(t *testing.T) {
	subCodes := []Code{
		CodeSubscriptionNotFound,
		CodeSubscriptionSoftDeleted,
		CodeSubscriptionForbidden,
		CodeSubscriptionInvalidStatus,
		CodeSubscriptionInvalidTransition,
		CodeSubscriptionUnknownState,
	}
	for _, code := range subCodes {
		entry, ok := Default.Lookup(code)
		if !ok {
			t.Fatalf("code %q not found", code)
		}
		if entry.HTTPStatus < 400 || entry.HTTPStatus > 499 {
			t.Errorf("subscription code %q should have 4xx status, got %d", code, entry.HTTPStatus)
		}
	}
}

func TestServerCodesHave5xxStatuses(t *testing.T) {
	serverCodes := []Code{
		CodeInternalError,
		CodeBillingParseError,
	}
	for _, code := range serverCodes {
		entry, ok := Default.Lookup(code)
		if !ok {
			t.Fatalf("code %q not found", code)
		}
		if entry.HTTPStatus < 500 || entry.HTTPStatus > 599 {
			t.Errorf("server code %q should have 5xx status, got %d", code, entry.HTTPStatus)
		}
	}
}
