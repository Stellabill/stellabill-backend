package logger

import (
    "os"

    "github.com/gin-gonic/gin"
    "github.com/sirupsen/logrus"
    "go.opentelemetry.io/contrib/bridges/otellogrus"
)

var Log = logrus.New()

func Init() {
    Log.SetFormatter(&logrus.JSONFormatter{})
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
    return Log.WithFields(fields)
}

func SafePrintf(format string, args ...interface{}) {
    Log.Printf(format, args...)
}