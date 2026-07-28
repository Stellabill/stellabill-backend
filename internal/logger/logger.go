package logger

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

// Log is the package-level logrus instance shared by callers that want a
// pre-configured JSON logger.
var Log = logrus.New()

// Logger is the interface that statement archive job depends on.
type Logger interface {
	Error(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Info(msg string, args ...interface{})
}

// SafePrintf logs a formatted message at Info level.
func SafePrintf(format string, args ...interface{}) {
	Log.Infof(format, args...)
	fmt.Printf(format+"\n", args...)
}
