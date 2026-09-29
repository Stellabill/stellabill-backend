package service_test

import (
	"errors"
	"fmt"
	"testing"

	"stellarbill-backend/internal/errcode"
	"stellarbill-backend/internal/service"
)

func TestErrNotFoundIsRegistered(t *testing.T) {
	code, found := errcode.MustLookup(service.ErrNotFound)
	if !found {
		t.Error("ErrNotFound must be registered in errcode")
	}
	if code != errcode.CodeNotFound {
		t.Errorf("expected %q for ErrNotFound, got %q", errcode.CodeNotFound, code)
	}
}

func TestErrDeletedIsRegistered(t *testing.T) {
	code, found := errcode.MustLookup(service.ErrDeleted)
	if !found {
		t.Error("ErrDeleted must be registered in errcode")
	}
	if code != errcode.CodeSubscriptionDeleted {
		t.Errorf("expected %q for ErrDeleted, got %q", errcode.CodeSubscriptionDeleted, code)
	}
}

func TestErrForbiddenIsRegistered(t *testing.T) {
	code, found := errcode.MustLookup(service.ErrForbidden)
	if !found {
		t.Error("ErrForbidden must be registered in errcode")
	}
	if code != errcode.CodeForbidden {
		t.Errorf("expected %q for ErrForbidden, got %q", errcode.CodeForbidden, code)
	}
}

func TestErrBillingParseIsRegistered(t *testing.T) {
	code, found := errcode.MustLookup(service.ErrBillingParse)
	if !found {
		t.Error("ErrBillingParse must be registered in errcode")
	}
	if code != errcode.CodeSubscriptionBillingParse {
		t.Errorf("expected %q for ErrBillingParse, got %q", errcode.CodeSubscriptionBillingParse, code)
	}
}

func TestErrExportInProgressIsRegistered(t *testing.T) {
	code, found := errcode.MustLookup(service.ErrExportInProgress)
	if !found {
		t.Error("ErrExportInProgress must be registered in errcode")
	}
	if code != errcode.CodeExportInProgress {
		t.Errorf("expected %q for ErrExportInProgress, got %q", errcode.CodeExportInProgress, code)
	}
}

func TestErrInvalidTransitionIsRegistered(t *testing.T) {
	code, found := errcode.MustLookup(service.ErrInvalidTransition)
	if !found {
		t.Error("ErrInvalidTransition must be registered in errcode")
	}
	if code != errcode.CodeSubscriptionInvalidTransition {
		t.Errorf("expected %q for ErrInvalidTransition, got %q", errcode.CodeSubscriptionInvalidTransition, code)
	}
}

func TestErrUnknownCurrentStateIsRegistered(t *testing.T) {
	code, found := errcode.MustLookup(service.ErrUnknownCurrentState)
	if !found {
		t.Error("ErrUnknownCurrentState must be registered in errcode")
	}
	if code != errcode.CodeSubscriptionUnknownState {
		t.Errorf("expected %q for ErrUnknownCurrentState, got %q", errcode.CodeSubscriptionUnknownState, code)
	}
}

func TestErrInvalidStatusIsRegistered(t *testing.T) {
	code, found := errcode.MustLookup(service.ErrInvalidStatus)
	if !found {
		t.Error("ErrInvalidStatus must be registered in errcode")
	}
	if code != errcode.CodeSubscriptionInvalidStatus {
		t.Errorf("expected %q for ErrInvalidStatus, got %q", errcode.CodeSubscriptionInvalidStatus, code)
	}
}

func TestErrInsufficientLiquidityIsRegistered(t *testing.T) {
	code, found := errcode.MustLookup(service.ErrInsufficientLiquidity)
	if !found {
		t.Error("ErrInsufficientLiquidity must be registered in errcode")
	}
	if code != errcode.CodeSwapInsufficientLiquidity {
		t.Errorf("expected %q for ErrInsufficientLiquidity, got %q", errcode.CodeSwapInsufficientLiquidity, code)
	}
}

func TestAllServiceErrorsHaveCodes(t *testing.T) {
	serviceErrors := []error{
		service.ErrNotFound,
		service.ErrDeleted,
		service.ErrForbidden,
		service.ErrBillingParse,
		service.ErrExportInProgress,
		service.ErrInvalidTransition,
		service.ErrUnknownCurrentState,
		service.ErrInvalidStatus,
	}
	for _, sentinel := range serviceErrors {
		code, found := errcode.MustLookup(sentinel)
		if !found {
			t.Errorf("service error %q is not registered in errcode", sentinel.Error())
			continue
		}
		if code == "" {
			t.Errorf("service error %q has empty code", sentinel.Error())
		}
	}
}

func TestLookupWrapsCorrectly(t *testing.T) {
	wrapped := fmt.Errorf("wrapped: %w", service.ErrNotFound)
	code := errcode.Lookup(wrapped)
	if code != errcode.CodeNotFound {
		t.Errorf("expected %q for wrapped ErrNotFound, got %q", errcode.CodeNotFound, code)
	}
}
