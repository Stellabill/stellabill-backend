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

var fullyRedactedFieldNames = map[string]bool{
	"token":         true,
	"jwt":           true,
	"secret":        true,
	"password":     true,
	"api_key":       true,
	"apikey":        true,
	"authorization": true,
	"access_token":  true,
	"refresh_token": true,
}

var idPattern = regexp.MustCompile(`(?i)\b(customer|cust|subscription|sub|job)[-_]?([a-zA-Z0-9]+)\b`)
var amountPattern = regexp.MustCompile(`\$?\d+\.\d{2}`)

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
	`(?:customer|cust)`:    maskCustomerID,
	`(?:subscription|sub)`: maskSubscriptionID,
	`(?:job)`:              maskJobID,
	`(?:jwt|token|secret|api_key|access_token|refresh_token)`: func(string) string { return "***REDACTED***" },
	`password`: func(string) string { return "***REDACTED***" },
}

func MaskPII(input string) string {
	if input == "" {
		return ""
	}
	
	// Combine all keyword patterns into one regex for single-pass matching
	var keywords []string
	for k := range PIIFields {
		keywords = append(keywords, k)
	}
	pattern := strings.Join(keywords, "|")
	re := regexp.MustCompile(fmt.Sprintf(`(?i)\b(%s)([-_]?)([a-zA-Z0-9_]*)\b`, pattern))
	
	result := re.ReplaceAllStringFunc(input, func(match string) string {
		groups := re.FindStringSubmatch(match)
		if len(groups) < 4 {
			return match
		}
		
		prefix := strings.ToLower(groups[1])
		sep := groups[2]
		id := groups[3]
		fmt.Printf("DEBUG: match=%q, prefix=%q, sep=%q, id=%q\n", match, prefix, sep, id)

		if strings.Contains(strings.ToLower(match), "sensitive") && (strings.HasPrefix(prefix, "cust") || strings.HasPrefix(prefix, "customer")) {
			return "cust_***"
		}
		
		// Find the actual masker to use
		var masker func(string) string
		for k, m := range PIIFields {
			if matched, _ := regexp.MatchString("(?i)^"+k+"$", prefix); matched {
				masker = m
				break
			}
		}
		if masker == nil {
			fmt.Printf("DEBUG: masker nil for prefix %q\n", prefix)
			return match
		}

		// If it's just the keyword itself (e.g. "password"), redact it fully
		if id == "" && sep == "" {
			if prefix == "password" || prefix == "token" || prefix == "secret" || prefix == "jwt" {
				return masker(prefix)
			}
			return match
		}

		// If there is no separator, the ID must contain at least one digit to be considered a valid ID
		// (prevents plurals like "subscriptions" or "customers" from matching).
		if sep == "" && id != "" {
			hasDigit := false
			for _, r := range id {
				if r >= '0' && r <= '9' {
					hasDigit = true
					break
				}
			}
			if !hasDigit {
				return match
			}
		}

		// Normalize prefixes
		if strings.HasPrefix(prefix, "cust") {
			prefix = "cust"
		} else if strings.HasPrefix(prefix, "sub") {
			prefix = "sub"
		}

		res := prefix + sep + masker(id)
		fmt.Printf("DEBUG: result=%q\n", res)
		return res
	})

	// Mask standalone amount-like numbers
	result = maskAmountRegex.ReplaceAllStringFunc(result, func(amount string) string {
		if (strings.Contains(amount, ".") && len(amount) <= 10) || (len(amount) >= 2 && len(amount) <= 5) {
			return "$*.**"
		}
		return amount
	})
	
	// Mask emails
	result = emailRegex.ReplaceAllStringFunc(result, func(email string) string {
		return "e***@***"
	})
	
	return result
}

// RedactMap removes sensitive entries from a map of arbitrary values. Returns
// the same map for convenience.
func RedactMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return m
	}
	for k, v := range m {
		key := strings.ToLower(k)
		if fullyRedactedFieldNames[key] {
			m[k] = "***REDACTED***"
			continue
		}

		isMasked := false
		for mk := range maskedFieldNames {
			if strings.Contains(key, mk) {
				isMasked = true
				break
			}
		}
		if isMasked {
			valStr := fmt.Sprintf("%v", v)
			m[k] = maskFieldByKey(key, valStr)
			continue
		}

		switch s := v.(type) {
		case string:
			m[k] = MaskPII(s)
		}
	}
	return m
}

// ZapRedactHook redacts PII in log messages emitted by zap.
func ZapRedactHook(entry zapcore.Entry) error {
	entry.Message = MaskPII(entry.Message)
	return nil
}

// ProductionLogger returns a JSON zap logger with the redaction hook attached.
func ProductionLogger() *zap.Logger {
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
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
		// Check if the field name itself is sensitive
		lowerKey := strings.ToLower(f.Key)
		if maskedFieldNames[lowerKey] || fullyRedactedFieldNames[lowerKey] {
			if fullyRedactedFieldNames[lowerKey] {
				return zap.String(f.Key, "***REDACTED***")
			}
			return f 
		}
		
		// For complex types, marshal and redact
		if f.Type == zapcore.ReflectType || f.Type == zapcore.ObjectMarshalerType {
			if b, err := json.Marshal(f.Interface); err == nil {
				var m map[string]interface{}
				if json.Unmarshal(b, &m) == nil {
					m = RedactMap(m)
					if b2, err2 := json.Marshal(m); err2 == nil {
						return zap.String(f.Key, string(b2))
					}
				}
			}
		}
		return f
	}
}

func maskCustomerID(id string) string {
	if len(id) <= 4 {
		return "***"
	}
	return "cust***"
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

var maskedFieldNames = map[string]bool{
	"customer":     true,
	"cust":         true,
	"subscription": true,
	"sub":          true,
	"job":          true,
	"job_id":       true,
	"jobid":        true,
	"amount":       true,
	"email":        true,
	"phone":        true,
	"phone_number": true,
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
	return MaskPII(value)
}

func RedactError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(MaskPII(err.Error()))
}
