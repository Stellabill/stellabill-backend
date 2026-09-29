package logger

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"stellarbill-backend/internal/security"

	"github.com/sirupsen/logrus"
)

var allowedLogSchemaKeys = map[string]bool{
	"time":           true,
	"timestamp":      true,
	"level":          true,
	"msg":            true,
	"request_id":     true,
	"trace_id":       true,
	"tenant_id":      true,
	"correlation_id": true,
	"method":         true,
	"path":           true,
	"status":         true,
	"latency_ms":     true,
	"client_ip":      true,
	"user_agent":     true,
	"panic":          true,
	"stack":          true,
	"error":          true,
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

// Log is the package-level logrus instance shared by callers that want a
// pre-configured JSON logger.
var Log = logrus.New()

func init() {
	Log.SetFormatter(NewLogSchemaFormatter(false))
}

// LogSchemaFormatter emits JSON with a stable structured schema while dropping
// unknown keys and redacting known sensitive fields before emission.
type LogSchemaFormatter struct {
	logrus.JSONFormatter
	strict bool
}

// NewLogSchemaFormatter creates a formatter that ensures the structured log schema
// is preserved across loggers and middleware output.
func NewLogSchemaFormatter(strict bool) logrus.Formatter {
	return &LogSchemaFormatter{JSONFormatter: logrus.JSONFormatter{TimestampFormat: time.RFC3339Nano}, strict: strict}
}

func (f *LogSchemaFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	fields := make(logrus.Fields, len(entry.Data)+8)
	for k, v := range entry.Data {
		if !allowedLogSchemaKeys[strings.ToLower(k)] && strings.TrimSpace(k) != "" {
			if f.strict {
				continue
			}
			if !strings.HasPrefix(k, "@") && !strings.HasPrefix(k, "_") {
				continue
			}
		}
		fields[k] = v
	}
	if _, ok := fields["time"]; !ok {
		fields["time"] = entry.Time.UTC().Format(time.RFC3339Nano)
	}
	if _, ok := fields["timestamp"]; !ok {
		fields["timestamp"] = fields["time"]
	}
	if _, ok := fields["level"]; !ok {
		fields["level"] = entry.Level.String()
	}
	if _, ok := fields["msg"]; !ok {
		fields["msg"] = entry.Message
	}
	redacted := map[string]interface{}{}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		val := fields[k]
		if s, ok := val.(string); ok {
			redacted[k] = security.RedactStringField(k, s)
			continue
		}
		if m, ok := val.(map[string]interface{}); ok {
			redacted[k] = security.RedactMap(m)
			continue
		}
		if m, ok := val.(logrus.Fields); ok {
			redacted[k] = security.RedactMap(map[string]interface{}(m))
			continue
		}
		redacted[k] = val
	}
	return json.Marshal(redacted)
}

// ValidateLogSchema checks that a log entry includes a time stamp and basic
// metadata required by the project schema while tolerating legacy `time` and
// `timestamp` names for compatibility.
func ValidateLogSchema(fields map[string]interface{}) error {
	if fields == nil {
		return fmt.Errorf("nil log fields")
	}
	if _, ok := fields["time"]; !ok {
		if _, ok = fields["timestamp"]; !ok {
			return fmt.Errorf("missing required timestamp field")
		}
	}
	if _, ok := fields["level"]; !ok {
		return fmt.Errorf("missing required level field")
	}
	if _, ok := fields["msg"]; !ok {
		return fmt.Errorf("missing required msg field")
	}
	return nil
}

// SafePrintf wraps logrus.Printf for callers migrating from the standard library log.
// Callers should pass already-redacted messages when logging potentially sensitive data.
func SafePrintf(format string, args ...interface{}) {
	Log.Print(fmt.Sprintf(format, args...))
}
