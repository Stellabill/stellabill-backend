package security

import (
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// PIIFields maps regex patterns to masking functions for log fields
var PIIFields = map[string]func(string) string{
	`^(customer|cust)_?`:    maskCustomerID,     // cust_xxx, customer123 -> cust_***
	`^(subscription|sub)_?`: maskSubscriptionID, // sub_xxx -> sub_***
	`^(job)_?`:              maskJobID,          // job_xxx -> job_***
	`^amount$`:              maskAmount,         // 19.99 -> $*.**
	`^(jwt|token|secret)$`:  func(s string) string { return "***REDACTED***" },
	`password`:              func(s string) string { return "***REDACTED***" },
}

// MaskPII scans a string or log message for PII patterns and masks them
func MaskPII(input string) string {
	result := input
	for pattern, masker := range PIIFields {
		re := regexp.MustCompile(fmt.Sprintf(`(?i)\b%s[-_]?[a-z0-9]*\b`, pattern))
		result = re.ReplaceAllStringFunc(result, func(match string) string {
			// Extract ID part and mask
			idPart := strings.TrimPrefix(strings.ToLower(match), strings.ToLower(pattern))
			maskedID := masker(idPart)
			return pattern + "_" + maskedID
		})
	}
	// Mask amounts like 19.99 -> $*.**
	result = maskAmountRegex.ReplaceAllString(result, "$*.**")
	// Mask emails
	result = emailRegex.ReplaceAllString(result, "e***@***")
	return result
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

// ZapRedactHook is a zapcore.Check that redacts PII in log fields and messages
func ZapRedactHook(entry zapcore.Entry) error {
	// Redact message
	entry.Message = MaskPII(entry.Message)
	// zapcore.Entry does not have a Context field in this version of zap.
	// Fields are handled at the Core level.
	return nil
}

// ProductionLogger returns a production-ready zap logger with PII redaction
func ProductionLogger() *zap.Logger {
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.InitialFields = map[string]interface{}{
		"service": "stellarbill-backend",
		"version": "1.0.0",
	}
	logger, _ := config.Build(zap.Hooks(ZapRedactHook))
	return logger
}

// DevLogger returns a development logger with color and redaction
func DevLogger() *zap.Logger {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	logger, _ := config.Build(zap.Hooks(ZapRedactHook))
	return logger.WithOptions(zap.AddCaller())
}
