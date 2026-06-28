package logger

import (
	"encoding/json"
	"os"
	"strings"

	"stellarbill-backend/internal/security"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/bridges/otellogrus"
)


// Logger is a minimal structured logging interface used by internal packages.
type Logger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}
var Log = logrus.New()

var requiredKeys = map[string]bool{
	"time":       true,
	"level":      true,
	"msg":        true,
	"request_id": true,
	"trace_id":   true,
	"tenant_id":  true,
}

var allowedKeys = map[string]bool{
	"time":           true,
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
}

type LogSchemaFormatter struct {
	inner   logrus.Formatter
	devMode bool
}

func NewLogSchemaFormatter(devMode bool) *LogSchemaFormatter {
	return &LogSchemaFormatter{
		inner:   &logrus.JSONFormatter{},
		devMode: devMode,
	}
}

func redactValue(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return security.MaskPII(val)
	case map[string]interface{}:
		return security.RedactMap(val)
	default:
		return val
	}
}

func (f *LogSchemaFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	filtered := logrus.Fields{}
	for k, v := range entry.Data {
		if f.devMode && !allowedKeys[k] {
			continue
		}
		filtered[k] = redactValue(v)
	}
	newEntry := *entry
	newEntry.Message = security.MaskPII(entry.Message)
	newEntry.Data = filtered
	return f.inner.Format(&newEntry)
}

func Init() {
	env := os.Getenv("ENV")
	devMode := strings.ToLower(env) == "development" || strings.ToLower(env) == "dev"
	Log.SetFormatter(NewLogSchemaFormatter(devMode))
	Log.SetOutput(os.Stdout)
	Log.AddHook(otellogrus.NewHook("stellabill-backend"))

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

// WithContextFields enriches log entries with request, correlation, and trace IDs from Gin context.
func WithContextFields(c *gin.Context) *logrus.Entry {
	fields := logrus.Fields{}
	if reqID := c.GetString("request_id"); reqID != "" {
		fields["request_id"] = reqID
	}
	if corrID := c.GetString("correlation_id"); corrID != "" {
		fields["correlation_id"] = corrID
	}
	if traceID := c.GetString("traceID"); traceID != "" {
		fields["trace_id"] = traceID
	}
	if tenantID := c.GetString("tenant_id"); tenantID != "" {
		fields["tenant_id"] = tenantID
	}
	return Log.WithFields(fields)
}

func SafePrintf(format string, args ...interface{}) {
	Log.Printf(format, args...)
}

// ParseLogEntry parses a JSON log entry into a map
func ParseLogEntry(data []byte) (map[string]interface{}, error) {
	var entry map[string]interface{}
	err := json.Unmarshal(data, &entry)
	return entry, err
}
