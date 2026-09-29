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
	"password":      true,
	"api_key":       true,
	"apikey":        true,
	"authorization": true,
	"x-admin-token": true,
	"x_admin_token": true,
	"admin_token":   true,
	"access_token":  true,
	"refresh_token": true,
	"client_secret": true,
	"private_key":   true,
	"session_token": true,
	"cookie":        true,
	"set-cookie":    true,
}

var (
	idPattern     = regexp.MustCompile(`(?i)\b(customer|cust|subscription|sub|job)[-_]?([a-zA-Z0-9]+)\b`)
	amountPattern = regexp.MustCompile(`\$?\d+\.\d{2}`)
)

func redactSensitiveFragments(s string) string {
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(authorization\s*[:=]\s*(?:bearer\s+)?)\S+`),
		regexp.MustCompile(`(?i)(x-admin-token\s*[:=]\s*)\S+`),
		regexp.MustCompile(`(?i)(jwt\s*[:=]\s*)[^\s"'&,;]+`),
		regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/-]+=*`),
		regexp.MustCompile(`(?i)(password\s*[:=]\s*["']?)[^\s"'&,;]+`),
		regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*["']?)[^\s"'&,;]+`),
		regexp.MustCompile(`(?i)(secret\s*[:=]\s*["']?)[^\s"'&,;]+`),
		regexp.MustCompile(`(?i)(token\s*[:=]\s*["']?)[^\s"'&,;]+`),
	}
	for _, re := range patterns {
		s = re.ReplaceAllString(s, `${1}***REDACTED***`)
	}
	if strings.Contains(lower, "jwt=") || strings.Contains(lower, "jwt:") || strings.Contains(lower, "eyj") {
		s = regexp.MustCompile(`(?i)(jwt\s*[:=]\s*["']?)(eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+)`).ReplaceAllString(s, `${1}***REDACTED***`)
	}
	return s
}

// MaskPII redacts simple PII patterns from a string.
func MaskPII(input string) string {
	if input == "" {
		return ""
	}
	out := redactSensitiveFragments(input)
	out = idPattern.ReplaceAllStringFunc(out, func(match string) string {
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
			m[k] = RedactStringField(k, s)
		case map[string]interface{}:
			m[k] = RedactMap(s)
		case []interface{}:
			for i, item := range s {
				if sm, ok := item.(map[string]interface{}); ok {
					s[i] = RedactMap(sm)
				} else if ss, ok := item.(string); ok {
					s[i] = RedactStringField(k, ss)
				}
			}
			m[k] = s
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
	return MaskPII(redactSensitiveFragments(value))
}
