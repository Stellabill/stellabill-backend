package handlers

import (
	"fmt"
	"net/http"
	"stellarbill-backend/internal/security"
	"stellarbill-backend/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ErrorCode represents a standardized error code
type ErrorCode string

const (
	// Client errors
	ErrorCodeBadRequest       ErrorCode = "BAD_REQUEST"
	ErrorCodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	ErrorCodeForbidden        ErrorCode = "FORBIDDEN"
	ErrorCodeNotFound         ErrorCode = "NOT_FOUND"
	ErrorCodeConflict         ErrorCode = "CONFLICT"
	ErrorCodeValidationFailed ErrorCode = "VALIDATION_FAILED"
	// ErrorCodeUnknownField is returned when a mutation request body contains a
	// field not defined in the API schema. See internal/decoder for details.
	ErrorCodeUnknownField ErrorCode = "UNKNOWN_FIELD"

	// Server errors
	ErrorCodeInternalError      ErrorCode = "INTERNAL_ERROR"
	ErrorCodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"
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

// MapServiceErrorToResponse maps domain service errors to HTTP status codes and error codes
func MapServiceErrorToResponse(err error) (int, ErrorCode, string) {
	switch err {
	case service.ErrNotFound:
		return http.StatusNotFound, ErrorCodeNotFound, "The requested resource was not found"
	case service.ErrDeleted:
		return http.StatusGone, ErrorCodeNotFound, "The requested resource has been deleted"
	case service.ErrForbidden:
		return http.StatusForbidden, ErrorCodeForbidden, "You do not have permission to access this resource"
	case service.ErrBillingParse:
		return http.StatusInternalServerError, ErrorCodeInternalError, "An internal error occurred while processing your request"
	default:
		return http.StatusInternalServerError, ErrorCodeInternalError, "An unexpected error occurred"
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
