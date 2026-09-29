package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"stellarbill-backend/internal/errcode"
	"stellarbill-backend/internal/security"
	"stellarbill-backend/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ErrorCode represents a standardized error code.
// It delegates to the errcode registry for stable identifiers.
type ErrorCode = errcode.Code

const (
	// Client errors
	ErrorCodeBadRequest       ErrorCode = errcode.CodeBadRequest
	ErrorCodeUnauthorized     ErrorCode = errcode.CodeUnauthorized
	ErrorCodeForbidden        ErrorCode = errcode.CodeForbidden
	ErrorCodeNotFound         ErrorCode = errcode.CodeNotFound
	ErrorCodeConflict         ErrorCode = errcode.CodeConflict
	ErrorCodeValidationFailed ErrorCode = errcode.CodeValidationFailed
	ErrorCodeUnknownField     ErrorCode = errcode.CodeUnknownField

	// Server errors
	ErrorCodeInternalError      ErrorCode = errcode.CodeInternalError
	ErrorCodeServiceUnavailable ErrorCode = errcode.CodeServiceUnavailable

	// Subscription-scoped error codes
	ErrorCodeSubscriptionNotFound          ErrorCode = errcode.CodeSubscriptionNotFound
	ErrorCodeSubscriptionDeleted           ErrorCode = errcode.CodeSubscriptionDeleted
	ErrorCodeSubscriptionForbidden         ErrorCode = errcode.CodeSubscriptionForbidden
	ErrorCodeSubscriptionInvalidTransition ErrorCode = errcode.CodeSubscriptionInvalidTransition
	ErrorCodeSubscriptionUnknownState      ErrorCode = errcode.CodeSubscriptionUnknownState
	ErrorCodeSubscriptionInvalidStatus     ErrorCode = errcode.CodeSubscriptionInvalidStatus
	ErrorCodeSubscriptionBillingParse      ErrorCode = errcode.CodeSubscriptionBillingParse

	// Export error codes
	ErrorCodeExportInProgress ErrorCode = errcode.CodeExportInProgress

	// Swap error codes
	ErrorCodeSwapInsufficientLiquidity ErrorCode = errcode.CodeSwapInsufficientLiquidity
)

// ErrorEnvelope represents a standardized error response
type ErrorEnvelope struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	TraceID string                 `json:"trace_id"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// ProblemDetails represents an RFC 7807 problem+json error envelope
type ProblemDetails struct {
	Type     string                 `json:"type,omitempty"`
	Title    string                 `json:"title,omitempty"`
	Status   int                    `json:"status"`
	Detail   string                 `json:"detail,omitempty"`
	Instance string                 `json:"instance,omitempty"`
	Code     string                 `json:"code,omitempty"`
	TraceID  string                 `json:"trace_id,omitempty"`
	Details  map[string]interface{} `json:"details,omitempty"`
}

// RespondWithError sends a standardized error response
func RespondWithError(c *gin.Context, statusCode int, code ErrorCode, message string) {
	RespondWithErrorDetails(c, statusCode, code, message, nil)
}

// RespondWithErrorDetails sends a standardized error response with additional details
func RespondWithErrorDetails(c *gin.Context, statusCode int, code ErrorCode, message string, details map[string]interface{}) {
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

// RenderProblem returns an RFC 7807 problem+json response.
// It sets Content-Type to application/problem+json unless Accept is application/json.
func RenderProblem(c *gin.Context, status int, code ErrorCode, detail string) {
	traceID := c.GetString("traceID")
	if traceID == "" {
		traceID = generateTraceID()
	}

	redactedDetail := security.MaskPII(detail)

	problem := ProblemDetails{
		Type:     "about:blank",
		Title:    string(code),
		Status:   status,
		Detail:   redactedDetail,
		Instance: c.Request.URL.Path,
		Code:     string(code),
		TraceID:  traceID,
	}

	accept := c.GetHeader("Accept")
	contentType := "application/problem+json; charset=utf-8"
	if accept == "application/json" {
		contentType = "application/json; charset=utf-8"
	}

	c.Header("Content-Type", contentType)
	c.JSON(status, problem)
}

// generateTraceID generates a unique trace ID for request tracking
func generateTraceID() string {
	return uuid.New().String()
}

// MapServiceErrorToResponse maps domain service errors to HTTP status codes and error codes.
func MapServiceErrorToResponse(err error) (int, ErrorCode, string) {
	code := errcode.Lookup(err)
	switch {
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, ErrorCode(code), "The requested resource was not found"
	case errors.Is(err, service.ErrDeleted):
		return http.StatusGone, ErrorCode(code), "The requested resource has been deleted"
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden, ErrorCode(code), "You do not have permission to access this resource"
	case errors.Is(err, service.ErrBillingParse):
		return http.StatusInternalServerError, ErrorCode(code), "An internal error occurred while processing your request"
	default:
		return http.StatusInternalServerError, ErrorCode(code), "An unexpected error occurred"
	}
}

// RespondWithValidationError sends a validation error response
func RespondWithValidationError(c *gin.Context, message string, details map[string]interface{}) {
	RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeValidationFailed, message, details)
}

// RespondWithAuthError sends an authentication error response
func RespondWithAuthError(c *gin.Context, message string) {
	RespondWithError(c, http.StatusUnauthorized, ErrorCodeUnauthorized, message)
}

// RespondWithNotFoundError sends a not found error response
func RespondWithNotFoundError(c *gin.Context, resource string) {
	message := fmt.Sprintf("%s not found", resource)
	RespondWithError(c, http.StatusNotFound, ErrorCodeNotFound, message)
}

// RespondWithInternalError sends an internal server error response
func RespondWithInternalError(c *gin.Context, message string) {
	RespondWithError(c, http.StatusInternalServerError, ErrorCodeInternalError, message)
}
