package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// ValidationError represents a single validation error
type ValidationError struct {
	Field   string      `json:"field"`
	Message string      `json:"message"`
	Value   interface{} `json:"value,omitempty"`
}

// ValidationResponse represents the standardized error response
type ValidationResponse struct {
	Error   string            `json:"error"`
	Details []ValidationError `json:"details"`
}

// BindAndValidate is a helper to bind and validate request data
func BindAndValidate(c *gin.Context, obj interface{}) bool {
	if err := c.ShouldBind(obj); err != nil {
		if errs, ok := err.(validator.ValidationErrors); ok {
			details := make([]ValidationError, len(errs))
			for i, e := range errs {
				details[i] = ValidationError{
					Field:   e.Field(),
					Message: fmt.Sprintf("validation failed on the '%s' tag", e.Tag()),
					Value:   e.Value(),
				}
			}
			c.AbortWithStatusJSON(http.StatusBadRequest, ValidationResponse{
				Error:   "validation_failed",
				Details: details,
			})
			return false
		}
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	return true
}

// ValidateQuery returns a middleware that validates query parameters into the given struct
func ValidateQuery[T any]() gin.HandlerFunc {
	return func(c *gin.Context) {
		var query T
		if err := c.ShouldBindQuery(&query); err != nil {
			handleValidationError(c, err)
			return
		}
		c.Set("query", query)
		c.Next()
	}
}

// ValidatePath returns a middleware that validates path parameters into the given struct
func ValidatePath[T any]() gin.HandlerFunc {
	return func(c *gin.Context) {
		var path T
		if err := c.ShouldBindUri(&path); err != nil {
			handleValidationError(c, err)
			return
		}
		c.Set("path", path)
		c.Next()
	}
}

func handleValidationError(c *gin.Context, err error) {
	if errs, ok := err.(validator.ValidationErrors); ok {
		details := make([]ValidationError, len(errs))
		for i, e := range errs {
			details[i] = ValidationError{
				Field:   e.Field(),
				Message: fmt.Sprintf("validation failed on the '%s' tag", e.Tag()),
				Value:   e.Value(),
			}
		}
		c.AbortWithStatusJSON(http.StatusBadRequest, ValidationResponse{
			Error:   "validation_failed",
			Details: details,
		})
		return
	}
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}
