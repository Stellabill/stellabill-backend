package handlers

import (
	"fmt"
	"net/http"
	"stellarbill-backend/internal/errcode"
	"stellarbill-backend/internal/security"
	"stellarbill-backend/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ErrorCode is an alias for errcode.Code, preserving backward compatibility
// while delegating to the central error code registry.
type ErrorCode = errcode.Code

// Canonical error code constants. Each maps to a stable <domain>/<operation>
// string in the errcode registry. Handlers should prefer the errcode.Code
// constants directly; these aliases exist for callers that reference the old
// names.
const (
	// Client errors
	ErrorCodeBadRequest       = errcode.CodeBadRequest
	ErrorCodeUnauthorized     = errcode.CodeAuthMissing
	ErrorCodeForbidden        = errcode.CodeAuthForbidden
	ErrorCodeNotFound         = errcode.CodeNotFound
	ErrorCodeConflict         = errcode.CodeConflict
	ErrorCodeValidationFailed = errcode.CodeValidationFailed
	// ErrorCodeUnknownField is returned when a mutation request body contains a
	// field not defined in the API schema. See internal/decoder for details.
	ErrorCodeUnknownField = errcode.CodeValidationUnknownField

	// Server errors
	ErrorCodeInternalError      = errcode.CodeInternalError
	ErrorCodeServiceUnavailable = errcode.CodeServiceUnavailable
)

// ErrorEnvelope represents a standardized error response
type ErrorEnvelope struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	TraceID string                 `json:"trace_id"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// RespondWithError sends a standardized error response
func RespondWithError(c *gin.Context, statusCode int, code errcode.Code, message string) {
	RespondWithErrorDetails(c, statusCode, code, message, nil)
}

// RespondWithErrorDetails sends a standardized error response with additional details
func RespondWithErrorDetails(c *gin.Context, statusCode int, code errcode.Code, message string, details map[string]interface{}) {
	c.Header("Content-Type", "application/json; charset=utf-8")

	traceID := c.GetString("traceID")
	if traceID == "" {
		// Generate trace ID if not already set
		traceID = generateTraceID()
	}

	// Redact message and details to prevent PII leakage
	redactedMessage := security.MaskPII(message)
	if details != nil {
		details = security.RedactMap(details)
	}

	envelope := ErrorEnvelope{
		Code:    string(code),
		Message: redactedMessage,
		TraceID: traceID,
		Details: details,
	}

	c.JSON(statusCode, envelope)
}

// generateTraceID generates a unique trace ID for request tracking
func generateTraceID() string {
	return uuid.New().String()
}

// MapServiceErrorToResponse maps domain service errors to HTTP status codes
// and domain-specific error codes from the errcode registry.
func MapServiceErrorToResponse(err error) (int, errcode.Code, string) {
	switch err {
	case service.ErrNotFound:
		return http.StatusNotFound, errcode.CodeSubscriptionNotFound, "The requested subscription was not found"
	case service.ErrDeleted:
		return http.StatusGone, errcode.CodeSubscriptionSoftDeleted, "The requested subscription has been deleted"
	case service.ErrForbidden:
		return http.StatusForbidden, errcode.CodeSubscriptionForbidden, "You do not have permission to access this subscription"
	case service.ErrBillingParse:
		return http.StatusInternalServerError, errcode.CodeBillingParseError, "An internal error occurred while processing billing data"
	case service.ErrInvalidTransition:
		return http.StatusConflict, errcode.CodeSubscriptionInvalidTransition, "The requested status transition is not allowed"
	case service.ErrUnknownCurrentState:
		return http.StatusConflict, errcode.CodeSubscriptionUnknownState, "The subscription has an unrecognized current status"
	case service.ErrInvalidStatus:
		return http.StatusUnprocessableEntity, errcode.CodeSubscriptionInvalidStatus, "The provided status value is not valid"
	case service.ErrExportInProgress:
		return http.StatusConflict, errcode.CodeExportInProgress, "An export is already in progress for this tenant"
	default:
		return http.StatusInternalServerError, errcode.CodeInternalError, "An unexpected error occurred"
	}
}

// RespondWithValidationError sends a validation error response
func RespondWithValidationError(c *gin.Context, message string, details map[string]interface{}) {
	RespondWithErrorDetails(c, http.StatusBadRequest, errcode.CodeValidationFailed, message, details)
}

// RespondWithAuthError sends an authentication error response
func RespondWithAuthError(c *gin.Context, message string) {
	RespondWithError(c, http.StatusUnauthorized, errcode.CodeAuthMissing, message)
}

// RespondWithNotFoundError sends a not found error response
func RespondWithNotFoundError(c *gin.Context, resource string) {
	message := fmt.Sprintf("%s not found", resource)
	RespondWithError(c, http.StatusNotFound, errcode.CodeNotFound, message)
}

// RespondWithInternalError sends an internal server error response
func RespondWithInternalError(c *gin.Context, message string) {
	RespondWithError(c, http.StatusInternalServerError, errcode.CodeInternalError, message)
}
