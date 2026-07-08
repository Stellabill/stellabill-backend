package security

import (
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

// MaskPII redacts simple PII patterns from a string.
func MaskPII(input string) string {
	if input == "" {
		return ""
	}
	out := idPattern.ReplaceAllStringFunc(input, func(match string) string {
		sub := idPattern.FindStringSubmatch(match)
		if len(sub) > 2 {
			prefix := strings.ToLower(sub[1])
			return prefix + "_***"
		}
		return match
	})
	out = amountPattern.ReplaceAllString(out, "$*.**")
	return out
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
	return logger
}

// DevLogger returns a development zap logger with no redaction.
func DevLogger() *zap.Logger {
	config := zap.NewDevelopmentConfig()
	logger, _ := config.Build()
	return logger
}

// RedactStringField redacts a single string value based on its key name.
func RedactStringField(key, value string) string {
	if fullyRedactedFieldNames[strings.ToLower(key)] {
		return "***REDACTED***"
	}
	return MaskPII(value)
}
