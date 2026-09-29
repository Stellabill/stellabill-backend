package security

import (
	"encoding/json"
	"errors"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var schemaAllowedTopLevel = map[string]bool{
	"timestamp":      true,
	"time":           true,
	"level":          true,
	"msg":            true,
	"request_id":     true,
	"trace_id":       true,
	"tenant_id":      true,
	"method":         true,
	"path":           true,
	"status":         true,
	"latency_ms":     true,
	"client_ip":      true,
	"user_agent":     true,
	"correlation_id": true,
	"error":          true,
	"panic":          true,
	"stack":          true,
	"authorization":  true,
	"x-admin-token":  true,
	"x_admin_token":  true,
	"admin_token":    true,
	"token":          true,
	"jwt":            true,
	"password":       true,
	"api_key":        true,
	"apikey":         true,
	"secret":         true,
	"access_token":   true,
	"refresh_token":  true,
}

// FilterLogFields drops unknown top-level keys and preserves the required structured-log contract.
func FilterLogFields(fields []zapcore.Field) []zapcore.Field {
	if len(fields) == 0 {
		return fields
	}
	filtered := make([]zapcore.Field, 0, len(fields))
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		keyLower := strings.ToLower(key)
		if keyLower == "timestamp" || keyLower == "time" || schemaAllowedTopLevel[keyLower] {
			filtered = append(filtered, field)
		}
	}
	return filtered
}

// RedactingCore wraps a zapcore.Core and redacts PII from fields before encoding.
type RedactingCore struct {
	inner zapcore.Core
}

// NewRedactingCore creates a core that redacts PII before passing to the inner core.
func NewRedactingCore(inner zapcore.Core) *RedactingCore {
	return &RedactingCore{inner: inner}
}

// Check implements zapcore.Core.
func (c *RedactingCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	return c.inner.Check(entry, ce)
}

// Enabled implements zapcore.Core.
func (c *RedactingCore) Enabled(level zapcore.Level) bool {
	return c.inner.Enabled(level)
}

// Write implements zapcore.Core with field redaction.
func (c *RedactingCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	entry.Message = MaskPII(entry.Message)
	redacted := RedactZapCoreFields(FilterLogFields(fields))
	return c.inner.Write(entry, redacted)
}

// Sync implements zapcore.Core.
func (c *RedactingCore) Sync() error {
	return c.inner.Sync()
}

// With implements zapcore.Core.
func (c *RedactingCore) With(fields []zapcore.Field) zapcore.Core {
	redacted := RedactZapCoreFields(FilterLogFields(fields))
	return &RedactingCore{inner: c.inner.With(redacted)}
}

// RedactZapCoreField redacts a single zapcore.Field.
func RedactZapCoreField(field zapcore.Field) zapcore.Field {
	switch field.Type {
	case zapcore.StringType:
		redacted := RedactStringField(field.Key, field.String)
		return zapcore.Field{
			Key:    field.Key,
			Type:   zapcore.StringType,
			String: redacted,
		}
	case zapcore.ErrorType:
		if err, ok := field.Interface.(error); ok {
			return zapcore.Field{
				Key:       field.Key,
				Type:      zapcore.ErrorType,
				Interface: errors.New(MaskPII(err.Error())),
			}
		}
		return field
	case zapcore.ReflectType:
		if b, err := json.Marshal(field.Interface); err == nil {
			var m map[string]interface{}
			if json.Unmarshal(b, &m) == nil {
				m = RedactMap(m)
				return zapcore.Field{
					Key:       field.Key,
					Type:      zapcore.ReflectType,
					Interface: m,
				}
			}
		}
		return field
	default:
		return field
	}
}

// RedactZapCoreFields redacts a slice of zapcore.Field.
func RedactZapCoreFields(fields []zapcore.Field) []zapcore.Field {
	if len(fields) == 0 {
		return fields
	}
	redacted := make([]zapcore.Field, 0, len(fields))
	for _, f := range fields {
		redacted = append(redacted, RedactZapCoreField(f))
	}
	return redacted
}

// MustRedactLogger wraps a zap.Logger to automatically redact PII from all log fields.
func MustRedactLogger(logger *zap.Logger) *zap.Logger {
	if logger == nil {
		return nil
	}
	return logger.WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		return NewRedactingCore(c)
	}))
}

// Ensure ProductionLogger and DevLogger use redaction for fields by default.
func init() {
}

// RedactProductionLogger returns a production logger with PII redaction on both messages and fields.
func RedactProductionLogger() *zap.Logger {
	logger := ProductionLogger()
	return MustRedactLogger(logger)
}

// RedactDevLogger returns a development logger with PII redaction on both messages and fields.
func RedactDevLogger() *zap.Logger {
	logger := DevLogger()
	return MustRedactLogger(logger)
}
