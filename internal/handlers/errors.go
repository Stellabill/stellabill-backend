package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/validation"
)

// ErrorCode represents a standardized error code
type ErrorCode string

const (
	// Client errors
	ErrorCodeBadRequest     ErrorCode = "BAD_REQUEST"
	ErrorCodeUnauthorized   ErrorCode = "UNAUTHORIZED"
	ErrorCodeForbidden      ErrorCode = "FORBIDDEN"
	ErrorCodeNotFound       ErrorCode = "NOT_FOUND"
	ErrorCodeConflict       ErrorCode = "CONFLICT"
	ErrorCodeValidationFailed ErrorCode = "VALIDATION_FAILED"

	// Server errors
	ErrorCodeInternalError ErrorCode = "INTERNAL_ERROR"
	ErrorCodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"
)

// ErrorEnvelope represents a standardized error response
type ErrorEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"trace_id"`
	Details map[string]interface{} `json:"details,omitempty"`
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

	envelope := ErrorEnvelope{
		Code:    string(code),
		Message: message,
		TraceID: traceID,
		Details: details,
	}

	c.JSON(statusCode, envelope)
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

// RespondWithValidationError is kept for compatibility but delegates to RespondWithValidationFields
func RespondWithValidationError(c *gin.Context, message string, fieldErrors []validation.FieldError) {
	RespondWithValidationFields(c, message, fieldErrors)
}

// RespondWithValidationFields sends a validation error response with the specific {error, fields} format
func RespondWithValidationFields(c *gin.Context, message string, fields []validation.FieldError) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.JSON(http.StatusBadRequest, gin.H{
		"error":  message,
		"fields": fields,
	})
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
