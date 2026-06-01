package validation

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"golang.org/x/text/unicode/norm"
)

var validate *validator.Validate

func init() {
	validate = validator.New()

	// Use json tag names for errors
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
}

// FieldError represents a single validation error
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// BindAndValidateJSON binds the request body to a struct and validates it
func BindAndValidateJSON(c *gin.Context, s interface{}) []FieldError {
	if err := c.ShouldBindJSON(s); err != nil {
		return []FieldError{{Field: "body", Message: "Invalid JSON payload"}}
	}

	return ValidateStruct(s)
}

// ValidateStruct validates a struct and returns a slice of FieldErrors
func ValidateStruct(s interface{}) []FieldError {
	err := validate.Struct(s)
	if err == nil {
		return nil
	}

	var fieldErrors []FieldError
	if ve, ok := err.(validator.ValidationErrors); ok {
		for _, fe := range ve {
			fieldErrors = append(fieldErrors, FieldError{
				Field:   fe.Field(),
				Message: formatErrorMessage(fe),
			})
		}
	} else {
		fieldErrors = append(fieldErrors, FieldError{
			Field:   "struct",
			Message: err.Error(),
		})
	}

	return fieldErrors
}

// ValidateVar validates a single variable using tags
func ValidateVar(v interface{}, tag string) []FieldError {
	err := validate.Var(v, tag)
	if err == nil {
		return nil
	}

	var fieldErrors []FieldError
	if ve, ok := err.(validator.ValidationErrors); ok {
		for _, fe := range ve {
			field := fe.Field()
			if field == "" {
				field = "value"
			}
			fieldErrors = append(fieldErrors, FieldError{
				Field:   field,
				Message: formatErrorMessage(fe),
			})
		}
	} else {
		fieldErrors = append(fieldErrors, FieldError{
			Field:   "value",
			Message: err.Error(),
		})
	}

	return fieldErrors
}

// ValidateUUID checks if a string is a valid UUID
func ValidateUUID(id string) error {
	return validate.Var(id, "required,uuid")
}

// formatErrorMessage creates a human-readable error message
func formatErrorMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", fe.Field())
	case "email":
		return fmt.Sprintf("%s must be a valid email address", fe.Field())
	case "uuid":
		return fmt.Sprintf("%s must be a valid UUID", fe.Field())
	case "min":
		return fmt.Sprintf("%s must be at least %s", fe.Field(), fe.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", fe.Field(), fe.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of [%s]", fe.Field(), fe.Param())
	case "datetime":
		return fmt.Sprintf("%s must be a valid date-time (RFC3339)", fe.Field())
	default:
		return fmt.Sprintf("%s failed validation on the %s tag", fe.Field(), fe.Tag())
	}
}

// NormalizeString normalizes a string using NFKC to prevent homograph attacks
func NormalizeString(s string) string {
	return norm.NFKC.String(s)
}
