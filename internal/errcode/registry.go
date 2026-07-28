// Package errcode provides a stable, structured error code registry for the
// Stellabill API. Every error response emitted by the API carries a code from
// this registry, enabling clients to branch on stable identifiers rather than
// fragile human-readable message strings.
//
// Codes follow the pattern <domain>/<operation-error> and are grouped by
// problem domain. The registry is validated at init time so that:
//   - every code is unique across the entire registry,
//   - no code is the empty string.
//
// New error codes MUST be registered in this file before they can be used by
// handlers. This keeps the public contract auditable and prevents typos from
// reaching production responses.
package errcode

import (
	"fmt"
	"sort"
	"sync"
)

// Code is a stable error identifier returned in every API error response body.
// Clients should switch on Code, never on the human-readable message string.
type Code string

// Entry describes a single registered error code with its default message and
// HTTP status code.
type Entry struct {
	Code       Code   `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status"`
}

// Registry holds all known error codes. It is safe for concurrent reads but
// writes (Register) must only happen during init or test setup.
type Registry struct {
	mu    sync.RWMutex
	codes map[Code]Entry
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{codes: make(map[Code]Entry)}
}

// Register adds a code to the registry. It panics if the code is empty or has
// already been registered, which makes misconfiguration fail at startup.
func (r *Registry) Register(code Code, message string, httpStatus int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if code == "" {
		panic("errcode: empty code")
	}
	if _, exists := r.codes[code]; exists {
		panic(fmt.Sprintf("errcode: duplicate code %q", code))
	}
	r.codes[code] = Entry{Code: code, Message: message, HTTPStatus: httpStatus}
}

// Lookup returns the Entry for the given code, or an empty Entry and false if
// the code is not registered.
func (r *Registry) Lookup(code Code) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.codes[code]
	return e, ok
}

// Codes returns all registered codes in sorted order.
func (r *Registry) Codes() []Code {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Code, 0, len(r.codes))
	for c := range r.codes {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Len returns the number of registered codes.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.codes)
}

// All returns all registered entries in sorted order by code.
func (r *Registry) All() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, 0, len(r.codes))
	for _, e := range r.codes {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Default is the global error code registry populated at init time. All
// application code should reference codes from this registry.
var Default = New()

// --- Registration helper ---

func mustRegister(code Code, message string, httpStatus int) {
	Default.Register(code, message, httpStatus)
}

// ---------------------------------------------------------------------------
// Subscription domain
// ---------------------------------------------------------------------------

const (
	CodeSubscriptionNotFound         Code = "subscription/not-found"
	CodeSubscriptionSoftDeleted      Code = "subscription/soft-deleted"
	CodeSubscriptionForbidden        Code = "subscription/forbidden"
	CodeSubscriptionInvalidStatus    Code = "subscription/invalid-status"
	CodeSubscriptionInvalidTransition Code = "subscription/invalid-state-transition"
	CodeSubscriptionUnknownState     Code = "subscription/unknown-current-state"
)

// ---------------------------------------------------------------------------
// Billing domain
// ---------------------------------------------------------------------------

const (
	CodeBillingParseError Code = "billing/parse-error"
)

// ---------------------------------------------------------------------------
// Statement domain
// ---------------------------------------------------------------------------

const (
	CodeStatementNotFound Code = "statement/not-found"
	CodeStatementForbidden Code = "statement/forbidden"
)

// ---------------------------------------------------------------------------
// Plan domain
// ---------------------------------------------------------------------------

const (
	CodePlanNotFound Code = "plan/not-found"
)

// ---------------------------------------------------------------------------
// Swap domain
// ---------------------------------------------------------------------------

const (
	CodeSwapInsufficientLiquidity Code = "swap/insufficient-liquidity"
	CodeSwapInvalidAmount         Code = "swap/invalid-amount"
)

// ---------------------------------------------------------------------------
// Export domain
// ---------------------------------------------------------------------------

const (
	CodeExportInProgress Code = "export/in-progress"
)

// ---------------------------------------------------------------------------
// Webhook domain
// ---------------------------------------------------------------------------

const (
	CodeWebhookInvalidPayload   Code = "webhook/invalid-payload"
	CodeWebhookUnknownEventType Code = "webhook/unknown-event-type"
	CodeWebhookMissingField     Code = "webhook/missing-field"
)

// ---------------------------------------------------------------------------
// Authentication / authorization domain
// ---------------------------------------------------------------------------

const (
	CodeAuthMissing         Code = "auth/missing-credentials"
	CodeAuthInvalid         Code = "auth/invalid-token"
	CodeAuthForbidden       Code = "auth/forbidden"
	CodeAuthInsufficientPerm Code = "auth/insufficient-permissions"
)

// ---------------------------------------------------------------------------
// Validation domain
// ---------------------------------------------------------------------------

const (
	CodeValidationFailed Code = "validation/failed"
	CodeValidationUnknownField Code = "validation/unknown-field"
)

// ---------------------------------------------------------------------------
// Client errors (generic)
// ---------------------------------------------------------------------------

const (
	CodeBadRequest      Code = "client/bad-request"
	CodeNotFound        Code = "client/not-found"
	CodeConflict        Code = "client/conflict"
	CodeRateLimited     Code = "client/rate-limited"
	CodePayloadTooLarge Code = "client/payload-too-large"
)

// ---------------------------------------------------------------------------
// Server errors (generic)
// ---------------------------------------------------------------------------

const (
	CodeInternalError      Code = "system/internal-error"
	CodeServiceUnavailable Code = "system/service-unavailable"
)

func init() {
	// Subscription
	mustRegister(CodeSubscriptionNotFound, "The requested subscription was not found", 404)
	mustRegister(CodeSubscriptionSoftDeleted, "The requested subscription has been deleted", 410)
	mustRegister(CodeSubscriptionForbidden, "You do not have permission to access this subscription", 403)
	mustRegister(CodeSubscriptionInvalidStatus, "The provided status value is not valid", 422)
	mustRegister(CodeSubscriptionInvalidTransition, "The requested status transition is not allowed", 409)
	mustRegister(CodeSubscriptionUnknownState, "The subscription has an unrecognized current status", 409)

	// Billing
	mustRegister(CodeBillingParseError, "An internal error occurred while processing billing data", 500)

	// Statement
	mustRegister(CodeStatementNotFound, "The requested statement was not found", 404)
	mustRegister(CodeStatementForbidden, "You do not have permission to access this statement", 403)

	// Plan
	mustRegister(CodePlanNotFound, "The requested plan was not found", 404)

	// Swap
	mustRegister(CodeSwapInsufficientLiquidity, "Insufficient liquidity for the requested swap", 422)
	mustRegister(CodeSwapInvalidAmount, "The provided swap amount is invalid", 400)

	// Export
	mustRegister(CodeExportInProgress, "An export is already in progress for this tenant", 409)

	// Webhook
	mustRegister(CodeWebhookInvalidPayload, "The webhook payload is malformed or missing required fields", 400)
	mustRegister(CodeWebhookUnknownEventType, "The webhook event type is not recognized", 422)
	mustRegister(CodeWebhookMissingField, "A required field is missing from the webhook payload", 400)

	// Auth
	mustRegister(CodeAuthMissing, "Authentication credentials are required", 401)
	mustRegister(CodeAuthInvalid, "The provided authentication credentials are invalid", 401)
	mustRegister(CodeAuthForbidden, "You do not have permission to perform this action", 403)
	mustRegister(CodeAuthInsufficientPerm, "Your role does not have the required permission", 403)

	// Validation
	mustRegister(CodeValidationFailed, "The request failed validation", 400)
	mustRegister(CodeValidationUnknownField, "The request contains an unrecognized field", 400)

	// Client (generic)
	mustRegister(CodeBadRequest, "The request is malformed or contains invalid parameters", 400)
	mustRegister(CodeNotFound, "The requested resource was not found", 404)
	mustRegister(CodeConflict, "The request conflicts with the current state of the resource", 409)
	mustRegister(CodeRateLimited, "Too many requests; please retry later", 429)
	mustRegister(CodePayloadTooLarge, "The request payload exceeds the maximum allowed size", 413)

	// Server (generic)
	mustRegister(CodeInternalError, "An unexpected internal error occurred", 500)
	mustRegister(CodeServiceUnavailable, "The service is temporarily unavailable", 503)
}
