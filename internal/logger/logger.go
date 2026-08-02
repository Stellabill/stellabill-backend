package logger

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

// Log is the package-level logrus instance shared by callers that want a
// pre-configured JSON logger.
var Log = logrus.New()

// SafePrintf is a safe wrapper around fmt.Sprintf that logs to the package
// logger at info level. It's used by middleware that needs to avoid direct
// stdout logging.
func SafePrintf(format string, args ...interface{}) {
	Log.Infof(format, args...)
// SafePrintf wraps logrus.Printf for callers migrating from the standard library log.
// Callers should pass already-redacted messages when logging potentially sensitive data.
func SafePrintf(format string, args ...interface{}) {
	Log.Print(fmt.Sprintf(format, args...))
}
