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

// MaskPII scans a string or log message for PII patterns and masks them.
func MaskPII(input string) string {
	if input == "" {
		return ""
	}
	result := input

	// Mask common ID patterns like cust_xxx, sub_xxx, etc.
	idPatterns := []struct {
		prefix string
		masker func(string) string
	}{
		{"customer", maskCustomerID},
		{"cust", maskCustomerID},
		{"subscription", maskSubscriptionID},
		{"sub", maskSubscriptionID},
		{"job", maskJobID},
	}

	for _, p := range idPatterns {
		re := regexp.MustCompile(fmt.Sprintf(`(?i)\b%s[-_]?([a-zA-Z0-9]+)\b`, p.prefix))
		result = re.ReplaceAllStringFunc(result, func(match string) string {
			sub := re.FindStringSubmatch(match)
			if len(sub) > 1 {
				return p.prefix + "_" + p.masker(sub[1])
			}
			return match
		})
	}

	// Mask standalone amount-like numbers
	result = maskAmountRegex.ReplaceAllStringFunc(result, func(amount string) string {
		if len(amount) <= 10 && (strings.Contains(amount, ".") || len(amount) <= 5) {
			return "$*.**"
		}
		return amount
	})

	// Mask emails
	result = emailRegex.ReplaceAllStringFunc(result, func(email string) string {
		if strings.Contains(email, "@") {
			return "e***@***"
		}
		return email
	})

	// Mask secrets
	secretPatterns := []string{"jwt", "token", "secret", "api_key", "password"}
	for _, p := range secretPatterns {
		re := regexp.MustCompile(fmt.Sprintf(`(?i)\b%s[-_]?([a-zA-Z0-9._-]+)\b`, p))
		result = re.ReplaceAllStringFunc(result, func(match string) string {
			return p + "_" + "***REDACTED***"
		})
	}

	return result
}

// RedactMap recursively redacts sensitive keys and values from a map of string->any.
func RedactMap(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}
	for key, val := range data {
		lowerKey := strings.ToLower(key)
		fullyRedact := fullyRedactedFieldNames[lowerKey]
		mask := maskedFieldNames[lowerKey]

		if nestedMap, ok := val.(map[string]interface{}); ok {
			RedactMap(nestedMap)
			continue
		}

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
			if str, ok := val.(string); ok {
				data[key] = maskFieldByKey(lowerKey, str)
			}
		} else if str, ok := val.(string); ok {
			data[key] = MaskPII(str)
		}
	}
	return data
}

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
		return "e***@***"
	case strings.Contains(key, "phone"):
		return "***-***-****"
	default:
		return value
	}
}

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

func looksSensitiveValue(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	for _, pattern := range PIIValuePatterns {
		if pattern.MatchString(v) {
			return true
		}
	}
	return false
}

func RedactError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(MaskPII(err.Error()))
}

func ZapRedactHook(entry zapcore.Entry) error {
	entry.Message = MaskPII(entry.Message)
	return nil
}

func ProductionLogger() *zap.Logger {
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	logger, _ := config.Build(zap.Hooks(ZapRedactHook))
	return logger.WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		return NewRedactingCore(c)
	}))
}

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

func RedactZapFields(fields []zap.Field) []zap.Field {
	redacted := make([]zap.Field, 0, len(fields))
	for _, f := range fields {
		redacted = append(redacted, RedactZapField(f))
	}
	return redacted
}

func RedactZapField(f zap.Field) zap.Field {
	switch f.Type {
	case zapcore.StringType:
		return zap.String(f.Key, RedactStringField(f.Key, f.String))
	case zapcore.ErrorType:
		if err, ok := f.Interface.(error); ok {
			return zap.Error(RedactError(err))
		}
		return f
	default:
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
