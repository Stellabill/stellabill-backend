package validator

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

var validate *validator.Validate

func init() {
	validate = validator.New()

	// Register function to get the json tag name for errors
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
	Tag     string `json:"tag"`
	Param   string `json:"param,omitempty"`
	Message string `json:"message"`
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
				Tag:     fe.Tag(),
				Param:   fe.Param(),
				Message: formatErrorMessage(fe),
			})
		}
	} else {
		// This should not happen if s is a struct
		fieldErrors = append(fieldErrors, FieldError{
			Field:   "struct",
			Tag:     "internal",
			Message: err.Error(),
		})
	}

	return fieldErrors
}

// ValidateUUID checks if a string is a valid UUID
func ValidateUUID(id string) error {
	err := validate.Var(id, "required,uuid")
	if err != nil {
		return fmt.Errorf("invalid UUID format")
	}
	return nil
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
	default:
		return fmt.Sprintf("%s failed validation on the %s tag", fe.Field(), fe.Tag())
	}
}
