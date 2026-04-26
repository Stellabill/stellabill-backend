package logger

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"stellarbill-backend/internal/security"
)

var Log = logrus.New()

// LogrusHook redacts PII from log entries and their fields.
type LogrusHook struct{}

func (h *LogrusHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire redacts PII from the log entry's message and any string fields.
func (h *LogrusHook) Fire(entry *logrus.Entry) error {
	// Redact the main message
	entry.Message = security.MaskPII(entry.Message)

	// Redact fields in-place by key
	if entry.Data != nil {
		// Build temporary map[string]interface{} with string keys for redaction
		tmp := make(map[string]interface{}, len(entry.Data))
		for k, v := range entry.Data {
			tmp[string(k)] = v
		}
		// Apply redaction on the map
		security.RedactMap(tmp)
		// Write back to entry.Data using original keys
		for k := range entry.Data {
			if newV, ok := tmp[string(k)]; ok {
				entry.Data[k] = newV
			}
		}
	}
	return nil
}

func Init() {
	Log.SetFormatter(&logrus.JSONFormatter{})
	Log.SetOutput(os.Stdout)
	Log.AddHook(&LogrusHook{})
	// OpenTelemetry log bridge not essential for PII redaction; disabled to avoid version mismatch.
	// Log.AddHook(otellogrus.NewHook())

	level := os.Getenv("LOG_LEVEL")
	switch level {
	case "debug":
		Log.SetLevel(logrus.DebugLevel)
	case "warn":
		Log.SetLevel(logrus.WarnLevel)
	case "error":
		Log.SetLevel(logrus.ErrorLevel)
	default:
		Log.SetLevel(logrus.InfoLevel)
	}
}

// SafePrintf wraps logrus.Printf with automatic PII redaction.
// It's a convenience function for migrating from standard library log.
func SafePrintf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	Log.Print(security.MaskPII(msg))
}

// SafeInfof logs an info message with PII redaction.
func SafeInfof(format string, args ...interface{}) {
	Log.Infof(security.MaskPII(fmt.Sprintf(format, args...)))
}

// SafeErrorf logs an error with PII redaction.
func SafeErrorf(format string, args ...interface{}) {
	Log.Errorf(security.MaskPII(fmt.Sprintf(format, args...)))
}

// SafeWarnf logs a warning with PII redaction.
func SafeWarnf(format string, args ...interface{}) {
	Log.Warnf(security.MaskPII(fmt.Sprintf(format, args...)))
}

// SafeDebugf logs a debug message with PII redaction.
func SafeDebugf(format string, args ...interface{}) {
	Log.Debugf(security.MaskPII(fmt.Sprintf(format, args...)))
}