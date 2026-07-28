package logger

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

// Log is the package-level logrus instance shared by callers that want a
// pre-configured JSON logger.
var Log = logrus.New()

// SafePrintf wraps logrus.Printf for callers migrating from the standard library log.
// Callers should pass already-redacted messages when logging potentially sensitive data.
func SafePrintf(format string, args ...interface{}) {
	Log.Print(fmt.Sprintf(format, args...))
}
