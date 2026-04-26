package security

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// PIIFieldNames classifies fields that contain PII used for identification.
// Such fields are fully redacted in structured logging and error details.
var fullyRedactedFieldNames = map[string]bool{
	// Authentication & secrets - fully redact
	"token":         true,
	"jwt":           true,
	"secret":        true,
	"password":      true,
	"api_key":       true,
	"apikey":        true,
	"authorization": true,
	"auth_header":   true,
	"access_token":  true,
	"refresh_token": true,
}

// MaskedFieldNames are fields that contain identifiers and are partially masked.
var maskedFieldNames = map[string]bool{
	"customer":     true,
	"cust":         true,
	"subscription": true,
	"sub":          true,
	"job":          true,
	"job_id":       true,
	"jobid":        true,
	"amount":       true, // masked to $*.**
	// Email is also partially masked; treat similarly
	"email":        true,
	"phone":        true,
	"phone_number": true,
}

// PIIValuePatterns matches regex patterns that indicate sensitive values (tokens, base64, etc.)
var PIIValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^bearer\s+`),
	regexp.MustCompile(`(?i)^basic\s+`),
	regexp.MustCompile(`^[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+$`), // JWT-like
	regexp.MustCompile(`^[A-Z0-9]{20,}$`),                                  // API keys
}

// PIIFields maps regex patterns to masking functions for log message content.
// Used for unstructured log message scanning.
var PIIFields = map[string]func(string) string{
	`^(customer|cust)_?`:    maskCustomerID,     // cust_xxx -> cust_***
	`^(subscription|sub)_?`: maskSubscriptionID, // sub_xxx -> sub_***
	`^(job)_?`:              maskJobID,          // job_xxx -> job_***
	`^amount$`:              maskAmount,         // 19.99 -> $*.**
	`^(jwt|token|secret|api_key|access_token|refresh_token)$`: func(string) string { return "***REDACTED***" },
	`password`: func(string) string { return "***REDACTED***" },
}

// MaskPII scans a string or log message for PII patterns and masks them.
// It handles both field names within log messages and inline sensitive data.
func MaskPII(input string) string {
	if input == "" {
		return ""
	}
	result := input
	for pattern, masker := range PIIFields {
		re := regexp.MustCompile(fmt.Sprintf(`(?i)\b%s[-_]?[a-z0-9]*\b`, pattern))
		result = re.ReplaceAllStringFunc(result, func(match string) string {
			lower := strings.ToLower(match)
			// Extract the ID portion after the prefix
			idPart := strings.TrimPrefix(lower, strings.ToLower(pattern))
			maskedID := masker(idPart)
			// Preserve separator style if present
			if strings.Contains(match, "_") {
				return pattern + "_" + maskedID
			}
			return pattern + maskedID
		})
	}
	// Mask standalone amount-like numbers
	result = maskAmountRegex.ReplaceAllStringFunc(result, func(amount string) string {
		// Only mask if it looks like a currency amount (has decimal point or is standalone)
		if len(amount) <= 10 && (strings.Contains(amount, ".") || len(amount) <= 5) {
			return "$*.**"
		}
		return amount
	})
	// Mask emails
	result = emailRegex.ReplaceAllStringFunc(result, func(email string) string {
		parts := strings.Split(email, "@")
		if len(parts) == 2 {
			return "e***@***"
		}
		return email
	})
	return result
}

// RedactMap recursively redacts sensitive keys and values from a map of string->any.
// It modifies the map in-place and returns it for convenience.
func RedactMap(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}
	for key, val := range data {
		lowerKey := strings.ToLower(key)
		fullyRedact := fullyRedactedFieldNames[lowerKey]
		mask := maskedFieldNames[lowerKey]

		// Handle nested maps
		if nestedMap, ok := val.(map[string]interface{}); ok {
			RedactMap(nestedMap)
			continue
		}

		// Handle slices that may contain maps
		if slice, ok := val.([]interface{}); ok {
			for _, item := range slice {
				if itemMap, ok := item.(map[string]interface{}); ok {
					RedactMap(itemMap)
				}
			}
		}

		if fullyRedact {
			data[key] = "***REDACTED***"
		} else if mask {
			// Apply partial masking based on field type
			if str, ok := val.(string); ok {
				data[key] = maskFieldByKey(lowerKey, str)
			} else {
				// Non-string values for masked fields are left as-is or converted?
				// Keep as-is.
			}
		} else if str, ok := val.(string); ok {
			// For non-PII string fields, still scan for embedded PII patterns
			data[key] = MaskPII(str)
		}
	}
	return data
}

// maskFieldByKey applies appropriate masking based on the field's key.
func maskFieldByKey(key, value string) string {
	switch {
	case strings.Contains(key, "customer"):
		return maskCustomerID(value)
	case strings.Contains(key, "subscription") || strings.HasPrefix(key, "sub"):
		return maskSubscriptionID(value)
	case strings.HasPrefix(key, "job"):
		return maskJobID(value)
	case strings.Contains(key, "amount"):
		return maskAmount(value)
	case strings.Contains(key, "email"):
		// Mask email to e***@***
		if atIdx := strings.Index(value, "@"); atIdx > 0 {
			return "e***@***"
		}
		return value
	case strings.Contains(key, "phone"):
		// Mask phone to ***-***-****
		if len(value) >= 10 {
			return "***-***-****"
		}
		return "***"
	default:
		return value
	}
}

// RedactStringField redacts a single field value based on field name.
func RedactStringField(fieldName, value string) string {
	lower := strings.ToLower(fieldName)
	if fullyRedactedFieldNames[lower] {
		return "***REDACTED***"
	}
	if maskedFieldNames[lower] {
		return maskFieldByKey(lower, value)
	}
	if looksSensitiveValue(value) {
		return "***REDACTED***"
	}
	return MaskPII(value)
}

// Specific maskers
func maskCustomerID(id string) string {
	if len(id) <= 4 {
		return "***"
	}
	return id[:4] + "***"
}

func maskSubscriptionID(id string) string {
	if len(id) <= 4 {
		return "***"
	}
	return id[:4] + "***"
}

func maskJobID(id string) string {
	if len(id) <= 4 {
		return "***"
	}
	return id[:4] + "***"
}

func maskAmount(amount string) string {
	return "$*.**"
}

var (
	maskAmountRegex = regexp.MustCompile(`\b\d+\.?\d*\b`)
	emailRegex      = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
)

// LooksSensitiveValue checks if a string value appears to be a token or credential.
func looksSensitiveValue(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	for _, pattern := range PIIValuePatterns {
		if pattern.MatchString(v) {
			return true
		}
	}
	return false
}

// RedactError ensures error messages don't contain PII by redacting them.
func RedactError(err error) error {
	if err == nil {
		return nil
	}
	redactedMsg := MaskPII(err.Error())
	return errors.New(redactedMsg)
}

// ZapRedactHook is a zapcore.Check that redacts PII in log messages.
// Note: Field-level redaction requires zapcore.EncodableObjectHook or custom encoder.
func ZapRedactHook(entry zapcore.Entry) error {
	entry.Message = MaskPII(entry.Message)
	return nil
}

// ProductionLogger returns a production-ready zap logger with PII redaction on messages and fields.
func ProductionLogger() *zap.Logger {
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.InitialFields = map[string]interface{}{
		"service": "stellarbill-backend",
		"version": "1.0.0",
	}
	logger, _ := config.Build(zap.Hooks(ZapRedactHook))
	// Wrap with field redaction using zap.WrapCore
	return logger.WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		return NewRedactingCore(c)
	}))
}

// DevLogger returns a development logger with color and redaction
func DevLogger() *zap.Logger {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	logger, _ := config.Build(zap.Hooks(ZapRedactHook))
	return logger.WithOptions(
		zap.AddCaller(),
		zap.WrapCore(func(c zapcore.Core) zapcore.Core {
			return NewRedactingCore(c)
		}),
	)
}

// RedactZapFields redacts a slice of zap.Field, returning a new slice.
// It handles string fields, errors, and reflective objects.
func RedactZapFields(fields []zap.Field) []zap.Field {
	if len(fields) == 0 {
		return fields
	}
	redacted := make([]zap.Field, 0, len(fields))
	for _, f := range fields {
		redacted = append(redacted, RedactZapField(f))
	}
	return redacted
}

// RedactZapField redacts a single zap.Field.
func RedactZapField(f zap.Field) zap.Field {
	switch f.Type {
	case zapcore.StringType:
		val := f.String
		redactedVal := RedactStringField(f.Key, val)
		return zap.String(f.Key, redactedVal)
	case zapcore.ErrorType:
		if err, ok := f.Interface.(error); ok {
			return zap.Error(errors.New(MaskPII(err.Error())))
		}
		return f
	default:
		// For complex types, marshal and redact
		if b, err := json.Marshal(f.Interface); err == nil {
			var m map[string]interface{}
			if json.Unmarshal(b, &m) == nil {
				m = RedactMap(m)
				if b2, err2 := json.Marshal(m); err2 == nil {
					return zap.String(f.Key, string(b2))
				}
			}
		}
		return f
	}
}
